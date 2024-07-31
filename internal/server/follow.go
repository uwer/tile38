package server

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/resp"
	"github.com/tidwall/tile38/internal/log"
)

var errNoLongerFollowing = errors.New("no longer following")

const checksumsz = 512 * 1024

func (s *Server) cmdFollow(msg *Message) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]
	var ok bool
	var host, sport string

	if vs, host, ok = tokenval(vs); !ok || host == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if vs, sport, ok = tokenval(vs); !ok || sport == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if len(vs) != 0 {
		return NOMessage, errInvalidNumberOfArguments
	}
	host = strings.ToLower(host)
	sport = strings.ToLower(sport)
	var update bool
	if host == "no" && sport == "one" {
		update = s.config.followHost() != "" || s.config.followPort() != 0
		s.config.setFollowHost("")
		s.config.setFollowPort(0)
	} else {
		n, err := strconv.ParseUint(sport, 10, 64)
		if err != nil {
			return NOMessage, errInvalidArgument(sport)
		}
		port := int(n)
		update = s.config.followHost() != host || s.config.followPort() != port
		auth := s.config.leaderAuth()
		if update {
			s.mu.Unlock()
			conn, err := DialTimeout(fmt.Sprintf("%s:%d", host, port), time.Second*2)
			if err != nil {
				s.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow: %v", err)
			}
			defer conn.Close()
			if auth != "" {
				if err := s.followDoLeaderAuth(conn, auth); err != nil {
					return NOMessage, fmt.Errorf("cannot follow: %v", err)
				}
			}
			m, err := doServer(conn)
			if err != nil {
				s.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow: %v", err)
			}
			if m["id"] == "" {
				s.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow: invalid id")
			}
			if m["id"] == s.config.serverID() {
				s.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow self")
			}
			if m["following"] != "" {
				s.mu.Lock()
				return NOMessage, fmt.Errorf("cannot follow a follower")
			}
			s.mu.Lock()
		}
		s.config.setFollowHost(host)
		s.config.setFollowPort(port)
	}
	s.config.write(false)
	if update {
		s.followc.Add(1)
		if s.config.followHost() != "" {
			log.Infof("following new host '%s' '%s'.", host, sport)
			go s.follow(s.config.followHost(), s.config.followPort(),
				int(s.followc.Load()))
		} else {
			log.Infof("following no one")
		}
	}
	return OKMessage(msg, start), nil
}

// cmdReplConf is a command handler that sets replication configuration info
func (s *Server) cmdReplConf(msg *Message, client *Client) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]
	var ok bool
	var cmd, val string

	// Parse the message
	if vs, cmd, ok = tokenval(vs); !ok || cmd == "" {
		return NOMessage, errInvalidNumberOfArguments
	}
	if _, val, ok = tokenval(vs); !ok || val == "" {
		return NOMessage, errInvalidNumberOfArguments
	}

	// Switch on the command received
	switch cmd {
	case "listening-port":
		// Parse the port as an integer
		port, err := strconv.Atoi(val)
		if err != nil {
			return NOMessage, errInvalidArgument(val)
		}

		// Apply the replication port to the client and return
		s.connsmu.RLock()
		defer s.connsmu.RUnlock()
		for _, c := range s.conns {
			if c.remoteAddr == client.remoteAddr {
				c.mu.Lock()
				c.replPort = port
				c.mu.Unlock()
				return OKMessage(msg, start), nil
			}
		}
	case "ip-address":
		// Apply the replication ip to the client and return
		s.connsmu.RLock()
		defer s.connsmu.RUnlock()
		for _, c := range s.conns {
			if c.remoteAddr == client.remoteAddr {
				c.mu.Lock()
				c.replAddr = val
				c.mu.Unlock()
				return OKMessage(msg, start), nil
			}
		}
	}
	return NOMessage, fmt.Errorf("cannot find follower")
}

func doServer(conn *RESPConn) (map[string]string, error) {
	v, err := conn.Do("server")
	if err != nil {
		return nil, err
	}
	if v.Error() != nil {
		return nil, v.Error()
	}
	arr := v.Array()
	m := make(map[string]string)
	for i := 0; i < len(arr)/2; i++ {
		m[arr[i*2+0].String()] = arr[i*2+1].String()
	}
	return m, err
}

func (s *Server) followHandleCommand(args []string, followc int, w io.Writer) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if int(s.followc.Load()) != followc {
		return s.aofsz, errNoLongerFollowing
	}
	msg := &Message{Args: args}
	_, d, err := s.command(msg, nil)
	if err != nil {
		if commandErrIsFatal(err) {
			return s.aofsz, err
		}
	}
	switch msg.Command() {
	case "publish":
		// Avoid writing these commands to the AOF
	default:
		if err := s.writeAOF(args, &d); err != nil {
			return s.aofsz, err
		}
	}
	if len(s.aofbuf) > 10240 {
		s.flushAOF(false)
	}
	return s.aofsz, nil
}

func (s *Server) followDoLeaderAuth(conn *RESPConn, auth string) error {
	v, err := conn.Do("auth", auth)
	if err != nil {
		return err
	}
	if v.Error() != nil {
		return v.Error()
	}
	if v.String() != "OK" {
		return errors.New("cannot follow: auth no ok")
	}
	return nil
}

func (s *Server) followStep(host string, port int, followc int) error {
	if int(s.followc.Load()) != followc {
		return errNoLongerFollowing
	}
	s.mu.Lock()
	s.faofsz = 0
	s.fcup = false
	auth := s.config.leaderAuth()
	s.mu.Unlock()
	addr := fmt.Sprintf("%s:%d", host, port)

	// check if we are following self
	conn, err := DialTimeout(addr, time.Second*2)
	if err != nil {
		return fmt.Errorf("cannot follow: %v", err)
	}
	defer conn.Close()
	if auth != "" {
		if err := s.followDoLeaderAuth(conn, auth); err != nil {
			return fmt.Errorf("cannot follow: %v", err)
		}
	}
	m, err := doServer(conn)
	if err != nil {
		return fmt.Errorf("cannot follow: %v", err)
	}

	if m["id"] == "" {
		return fmt.Errorf("cannot follow: invalid id")
	}
	if m["id"] == s.config.serverID() {
		return fmt.Errorf("cannot follow self")
	}
	if m["following"] != "" {
		return fmt.Errorf("cannot follow a follower")
	}

	// verify checksum
	pos, err := s.followCheckSome(addr, followc, auth)
	if err != nil {
		return err
	}

	// Send the replication port to the leader
	p := s.config.announcePort()
	if p == 0 {
		p = s.port
	}
	v, err := conn.Do("replconf", "listening-port", p)
	if err != nil {
		return err
	}
	if v.Error() != nil {
		return v.Error()
	}
	if v.String() != "OK" {
		return errors.New("invalid response to replconf request")
	}

	// Send the replication ip to the leader
	ip := s.config.announceIP()
	if ip != "" {
		v, err := conn.Do("replconf", "ip-address", ip)
		if err != nil {
			return err
		}
		if v.Error() != nil {
			return v.Error()
		}
		if v.String() != "OK" {
			return errors.New("invalid response to replconf request")
		}
	}
	if s.opts.ShowDebugMessages {
		log.Debug("follow:", addr, ":replconf")
	}

	v, err = conn.Do("aof", pos)
	if err != nil {
		return err
	}
	if v.Error() != nil {
		return v.Error()
	}
	if v.String() != "OK" {
		return errors.New("invalid response to aof live request")
	}
	if s.opts.ShowDebugMessages {
		log.Debug("follow:", addr, ":read aof")
	}

	aofSize, err := strconv.ParseInt(m["aof_size"], 10, 64)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.faofsz = int(aofSize)
	s.mu.Unlock()

	caughtUp := pos >= aofSize
	if caughtUp {
		s.mu.Lock()
		s.fcup = true
		s.fcuponce = true
		s.mu.Unlock()
		log.Info("caught up")
	}

	nullw := io.Discard
	for {
		v, telnet, _, err := conn.rd.ReadMultiBulk()
		if err != nil {
			return err
		}
		vals := v.Array()
		if telnet || v.Type() != resp.Array {
			return errors.New("invalid multibulk")
		}
		svals := make([]string, len(vals))
		for i := 0; i < len(vals); i++ {
			svals[i] = vals[i].String()
		}

		aofsz, err := s.followHandleCommand(svals, followc, nullw)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.faofsz = aofsz
		s.mu.Unlock()
		if !caughtUp {
			if aofsz >= int(aofSize) {
				caughtUp = true
				s.mu.Lock()
				s.flushAOF(false)
				s.fcup = true
				s.fcuponce = true
				s.mu.Unlock()
				log.Info("caught up")
			}
		}

	}
}

func (s *Server) follow(host string, port int, followc int) {
	for {
		err := s.followStep(host, port, followc)
		if err == errNoLongerFollowing {
			return
		}
		if err != nil && err != io.EOF {
			log.Error("follow: " + err.Error())
		}
		time.Sleep(time.Second)
	}
}
