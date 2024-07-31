package server

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/geojson/geo"
	"github.com/tidwall/resp"
	"github.com/tidwall/tile38/internal/log"
	"github.com/tidwall/tinylru"
	lua "github.com/yuin/gopher-lua"
	luajson "layeh.com/gopher-json"
)

const (
	iniLuaPoolSize = 5
	maxLuaPoolSize = 1000
)

// For Lua os.clock() impl
var startedAt = time.Now()

var errShaNotFound = errors.New("sha not found")
var errCmdNotSupported = errors.New("command not supported in scripts")
var errNotLeader = errors.New("not the leader")
var errReadOnly = errors.New("read only")
var errCatchingUp = errors.New("catching up to leader")
var errNoLuasAvailable = errors.New("no interpreters available")
var errTimeout = errors.New("timeout")

// Go-routine-safe pool of read-to-go lua states
type lStatePool struct {
	m     sync.Mutex
	s     *Server
	saved []*lua.LState
	total int
}

// newPool returns a new pool of lua states
func (s *Server) newPool() *lStatePool {
	pl := &lStatePool{
		saved: make([]*lua.LState, iniLuaPoolSize),
		s:     s,
	}
	// Fill the pool with some ready handlers
	for i := 0; i < iniLuaPoolSize; i++ {
		pl.saved[i] = pl.New()
		pl.total++
	}
	return pl
}

func (pl *lStatePool) Get() (*lua.LState, error) {
	pl.m.Lock()
	defer pl.m.Unlock()
	n := len(pl.saved)
	if n == 0 {
		if pl.total >= maxLuaPoolSize {
			return nil, errNoLuasAvailable
		}
		pl.total++
		return pl.New(), nil
	}
	x := pl.saved[n-1]
	pl.saved = pl.saved[0 : n-1]
	return x, nil
}

// Prune removes some of the idle lua states from the pool
func (pl *lStatePool) Prune() {
	pl.m.Lock()
	n := len(pl.saved)
	if n > iniLuaPoolSize {
		// drop half of the idle states that is above the minimum
		dropNum := (n - iniLuaPoolSize) / 2
		if dropNum < 1 {
			dropNum = 1
		}
		newSaved := make([]*lua.LState, n-dropNum)
		copy(newSaved, pl.saved[dropNum:])
		pl.saved = newSaved
		pl.total -= dropNum
	}
	pl.m.Unlock()
}

func (pl *lStatePool) New() *lua.LState {
	// Prevent opening all Lua modules
	L := lua.NewState(lua.Options{SkipOpenLibs: true})

	allowedModules := []struct {
		moduleName string
		moduleFn   lua.LGFunction
	}{
		{lua.BaseLibName, openBaseSubset},
		{lua.TabLibName, lua.OpenTable},
		{lua.MathLibName, lua.OpenMath},
		{lua.StringLibName, lua.OpenString},
		{lua.OsLibName, openOsSubset}, // See below for impl, only opens clock/difftime
	}

	// Open non-vulnerable modules (i.e. NOT io/os)
	for _, pair := range allowedModules {
		if err := L.CallByParam(lua.P{
			Fn:      L.NewFunction(pair.moduleFn),
			NRet:    0,
			Protect: true,
		}, lua.LString(pair.moduleName)); err != nil {
			panic(err)
		}
	}

	getArgs := func(ls *lua.LState) (evalCmd string, args []string) {
		evalCmd = ls.GetGlobal("EVAL_CMD").String()

		// Trying to work with unknown number of args.
		// When we see empty arg we call it enough.
		for i := 1; ; i++ {
			if arg := ls.ToString(i); arg == "" {
				break
			} else {
				args = append(args, arg)
			}
		}
		return
	}
	call := func(ls *lua.LState) int {
		evalCmd, args := getArgs(ls)
		var numRet int
		if res, err := pl.s.luaTile38Call(evalCmd, args[0], args[1:]...); err != nil {
			ls.RaiseError("ERR %s", err.Error())
			numRet = 0
		} else {
			ls.Push(ConvertToLua(ls, res))
			numRet = 1
		}
		return numRet
	}
	pcall := func(ls *lua.LState) int {
		evalCmd, args := getArgs(ls)
		if res, err := pl.s.luaTile38Call(evalCmd, args[0], args[1:]...); err != nil {
			ls.Push(ConvertToLua(ls, resp.ErrorValue(err)))
		} else {
			ls.Push(ConvertToLua(ls, res))
		}
		return 1

	}
	errorReply := func(ls *lua.LState) int {
		tbl := L.CreateTable(0, 1)
		tbl.RawSetString("err", lua.LString(ls.ToString(1)))
		ls.Push(tbl)
		return 1
	}
	statusReply := func(ls *lua.LState) int {
		tbl := L.CreateTable(0, 1)
		tbl.RawSetString("ok", lua.LString(ls.ToString(1)))
		ls.Push(tbl)
		return 1
	}
	sha1hex := func(ls *lua.LState) int {
		shaSum := Sha1Sum(ls.ToString(1))
		ls.Push(lua.LString(shaSum))
		return 1
	}
	distanceTo := func(ls *lua.LState) int {
		dt := geo.DistanceTo(
			float64(ls.ToNumber(1)),
			float64(ls.ToNumber(2)),
			float64(ls.ToNumber(3)),
			float64(ls.ToNumber(4)))
		ls.Push(lua.LNumber(dt))
		return 1
	}
	var exports = map[string]lua.LGFunction{
		"call":         call,
		"pcall":        pcall,
		"error_reply":  errorReply,
		"status_reply": statusReply,
		"sha1hex":      sha1hex,
		"distance_to":  distanceTo,
	}
	L.SetGlobal("tile38", L.SetFuncs(L.NewTable(), exports))

	// Load json
	L.SetGlobal("json", L.Get(luajson.Loader(L)))

	// Prohibit creating new globals in this state
	lockNewGlobals := func(ls *lua.LState) int {
		ls.RaiseError("attempt to create global variable '%s'", ls.ToString(2))
		return 0
	}
	mt := L.CreateTable(0, 1)
	mt.RawSetString("__newindex", L.NewFunction(lockNewGlobals))
	L.SetMetatable(L.Get(lua.GlobalsIndex), mt)

	return L
}

func (pl *lStatePool) Put(L *lua.LState) {
	pl.m.Lock()
	pl.saved = append(pl.saved, L)
	pl.m.Unlock()
}

func (pl *lStatePool) Shutdown() {
	pl.m.Lock()
	for _, L := range pl.saved {
		L.Close()
	}
	pl.m.Unlock()
}

// Go-routine-safe map of compiled scripts
type lScriptMap struct {
	m       sync.Mutex
	scripts map[string]*lua.FunctionProto
	lru     tinylru.LRUG[string, *lua.FunctionProto]
}

func (sm *lScriptMap) Get(key string) (script *lua.FunctionProto, ok bool) {
	sm.m.Lock()
	script, ok = sm.scripts[key]
	if !ok {
		script, ok = sm.lru.Get(key)
	}
	sm.m.Unlock()
	return script, ok
}

func (sm *lScriptMap) Put(key string, script *lua.FunctionProto) {
	sm.m.Lock()
	sm.scripts[key] = script
	sm.m.Unlock()
}

func (sm *lScriptMap) PutLRU(key string, script *lua.FunctionProto) {
	sm.m.Lock()
	sm.lru.Set(key, script)
	sm.m.Unlock()
}

func (sm *lScriptMap) Flush() {
	sm.m.Lock()
	sm.scripts = make(map[string]*lua.FunctionProto)
	sm.lru.Clear()
	sm.m.Unlock()
}

// NewScriptMap returns a new map with lua scripts
func (s *Server) newScriptMap() *lScriptMap {
	return &lScriptMap{
		scripts: make(map[string]*lua.FunctionProto),
	}
}

// ConvertToLua converts RESP value to lua LValue
func ConvertToLua(L *lua.LState, val resp.Value) lua.LValue {
	if val.IsNull() {
		return lua.LFalse
	}
	switch val.Type() {
	case resp.Integer:
		return lua.LNumber(val.Integer())
	case resp.BulkString:
		return lua.LString(val.String())
	case resp.Error:
		tbl := L.CreateTable(0, 1)
		tbl.RawSetString("err", lua.LString(val.String()))
		return tbl
	case resp.SimpleString:
		tbl := L.CreateTable(0, 1)
		tbl.RawSetString("ok", lua.LString(val.String()))
		return tbl
	case resp.Array:
		tbl := L.CreateTable(len(val.Array()), 0)
		for _, item := range val.Array() {
			tbl.Append(ConvertToLua(L, item))
		}
		return tbl
	}
	return lua.LString("ERR: unknown RESP type: " + val.Type().String())
}

// ConvertToRESP convert lua LValue to RESP value
func ConvertToRESP(val lua.LValue) resp.Value {
	switch val.Type() {
	case lua.LTNil:
		return resp.NullValue()
	case lua.LTBool:
		if val == lua.LTrue {
			return resp.IntegerValue(1)
		}
		return resp.NullValue()
	case lua.LTNumber:
		float := float64(val.(lua.LNumber))
		if math.IsNaN(float) || math.IsInf(float, 0) {
			return resp.FloatValue(float)
		}
		return resp.IntegerValue(int(math.Floor(float)))
	case lua.LTString:
		return resp.StringValue(val.String())
	case lua.LTTable:
		var values []resp.Value
		var specialValues []resp.Value
		var cb func(lk lua.LValue, lv lua.LValue)
		tbl := val.(*lua.LTable)

		if tbl.Len() != 0 { // list
			cb = func(lk lua.LValue, lv lua.LValue) {
				values = append(values, ConvertToRESP(lv))
			}
		} else { // map
			cb = func(lk lua.LValue, lv lua.LValue) {
				if lk.Type() == lua.LTString {
					lks := lk.String()
					switch lks {
					case "ok":
						specialValues = append(specialValues, resp.SimpleStringValue(lv.String()))
					case "err":
						specialValues = append(specialValues, resp.ErrorValue(errors.New(lv.String())))
					}
				}
				values = append(values, resp.ArrayValue(
					[]resp.Value{ConvertToRESP(lk), ConvertToRESP(lv)}))
			}
		}
		tbl.ForEach(cb)
		if len(values) == 1 && len(specialValues) == 1 {
			return specialValues[0]
		}
		return resp.ArrayValue(values)
	}
	return resp.ErrorValue(errors.New("Unsupported lua type: " + val.Type().String()))
}

// ConvertToJSON converts lua LValue to JSON string
func ConvertToJSON(val lua.LValue) string {
	switch val.Type() {
	case lua.LTNil:
		return "null"
	case lua.LTBool:
		if val == lua.LTrue {
			return "true"
		}
		return "false"
	case lua.LTNumber:
		return val.String()
	case lua.LTString:
		if b, err := json.Marshal(val.String()); err != nil {
			panic(err)
		} else {
			return string(b)
		}
	case lua.LTTable:
		var values []string
		var cb func(lk lua.LValue, lv lua.LValue)
		var start, end string

		tbl := val.(*lua.LTable)
		if tbl.Len() != 0 { // list
			start = `[`
			end = `]`
			cb = func(lk lua.LValue, lv lua.LValue) {
				values = append(values, ConvertToJSON(lv))
			}
		} else { // map
			start = `{`
			end = `}`
			cb = func(lk lua.LValue, lv lua.LValue) {
				values = append(
					values, ConvertToJSON(lk)+`:`+ConvertToJSON(lv))
			}
		}
		tbl.ForEach(cb)
		return start + strings.Join(values, `,`) + end
	}
	return "Unsupported lua type: " + val.Type().String()
}

func luaSetRawGlobals(ls *lua.LState, tbl map[string]lua.LValue) {
	gt := ls.Get(lua.GlobalsIndex).(*lua.LTable)
	for key, val := range tbl {
		gt.RawSetString(key, val)
	}
}

// Sha1Sum returns a string with hex representation of sha1 sum of a given string
func Sha1Sum(s string) string {
	h := sha1.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// Replace newlines with literal \n since RESP errors cannot have newlines
func makeSafeErr(err error) error {
	return errors.New(strings.Replace(err.Error(), "\n", `\n`, -1))
}

// Run eval/evalro/evalna command or it's -sha variant
func (s *Server) cmdEvalUnified(scriptIsSha bool, msg *Message) (res resp.Value, err error) {
	start := time.Now()
	vs := msg.Args[1:]

	var ok bool
	var script, numkeysStr, key, arg string
	if vs, script, ok = tokenval(vs); !ok || script == "" {
		return NOMessage, errInvalidNumberOfArguments
	}

	if vs, numkeysStr, ok = tokenval(vs); !ok || numkeysStr == "" {
		return NOMessage, errInvalidNumberOfArguments
	}

	var i, numkeys uint64
	if numkeys, err = strconv.ParseUint(numkeysStr, 10, 64); err != nil {
		err = errInvalidArgument(numkeysStr)
		return
	}

	luaState, err := s.luapool.Get()
	if err != nil {
		return
	}
	luaDeadline := lua.LNil
	if msg.Deadline != nil {
		dlTime := msg.Deadline.GetDeadlineTime()
		ctx, cancel := context.WithDeadline(context.Background(), dlTime)
		defer cancel()
		luaState.SetContext(ctx)
		defer luaState.RemoveContext()
		luaDeadline = lua.LNumber(float64(dlTime.UnixNano()) / 1e9)
	}
	defer s.luapool.Put(luaState)

	keysTbl := luaState.CreateTable(int(numkeys), 0)
	for i = 0; i < numkeys; i++ {
		if vs, key, ok = tokenval(vs); !ok || key == "" {
			err = errInvalidNumberOfArguments
			return
		}
		keysTbl.Append(lua.LString(key))
	}

	argsTbl := luaState.CreateTable(len(vs), 0)
	for len(vs) > 0 {
		if vs, arg, ok = tokenval(vs); !ok || arg == "" {
			err = errInvalidNumberOfArguments
			return
		}
		argsTbl.Append(lua.LString(arg))
	}

	var shaSum string
	if scriptIsSha {
		shaSum = script
	} else {
		shaSum = Sha1Sum(script)
	}

	luaSetRawGlobals(
		luaState, map[string]lua.LValue{
			"KEYS":     keysTbl,
			"ARGV":     argsTbl,
			"DEADLINE": luaDeadline,
			"EVAL_CMD": lua.LString(msg.Command()),
		})

	compiled, ok := s.luascripts.Get(shaSum)
	var fn *lua.LFunction
	if ok {
		fn = &lua.LFunction{
			IsG: false,
			Env: luaState.Env,

			Proto:     compiled,
			GFunction: nil,
			Upvalues:  make([]*lua.Upvalue, 0),
		}
	} else if scriptIsSha {
		err = errShaNotFound
		return
	} else {
		fn, err = luaState.Load(strings.NewReader(script), "f_"+shaSum)
		if err != nil {
			return NOMessage, makeSafeErr(err)
		}
		s.luascripts.Put(shaSum, fn.Proto)
	}
	luaState.Push(fn)
	defer luaSetRawGlobals(
		luaState, map[string]lua.LValue{
			"KEYS":     lua.LNil,
			"ARGV":     lua.LNil,
			"DEADLINE": lua.LNil,
			"EVAL_CMD": lua.LNil,
		})
	if err := luaState.PCall(0, 1, nil); err != nil {
		if strings.Contains(err.Error(), "context deadline exceeded") {
			msg.Deadline.Check()
		}
		log.Debugf("%v", err.Error())
		return NOMessage, makeSafeErr(err)
	}
	ret := luaState.Get(-1) // returned value
	luaState.Pop(1)

	switch msg.OutputType {
	case JSON:
		var buf bytes.Buffer
		buf.WriteString(`{"ok":true`)
		buf.WriteString(`,"result":` + ConvertToJSON(ret))
		buf.WriteString(`,"elapsed":"` + time.Since(start).String() + "\"}")
		return resp.StringValue(buf.String()), nil
	case RESP:
		return ConvertToRESP(ret), nil
	}
	return NOMessage, nil
}

func (s *Server) cmdScriptLoad(msg *Message) (resp.Value, error) {
	start := time.Now()
	vs := msg.Args[1:]

	var ok bool
	var script string
	if _, script, ok = tokenval(vs); !ok || script == "" {
		return NOMessage, errInvalidNumberOfArguments
	}

	shaSum := Sha1Sum(script)

	luaState, err := s.luapool.Get()
	if err != nil {
		return NOMessage, err
	}
	defer s.luapool.Put(luaState)

	fn, err := luaState.Load(strings.NewReader(script), "f_"+shaSum)
	if err != nil {
		return NOMessage, makeSafeErr(err)
	}
	s.luascripts.Put(shaSum, fn.Proto)

	switch msg.OutputType {
	case JSON:
		var buf bytes.Buffer
		buf.WriteString(`{"ok":true`)
		buf.WriteString(`,"result":"` + shaSum + `"`)
		buf.WriteString(`,"elapsed":"` + time.Since(start).String() + "\"}")
		return resp.StringValue(buf.String()), nil
	case RESP:
		return resp.StringValue(shaSum), nil
	}
	return NOMessage, nil
}

func (s *Server) cmdScriptExists(msg *Message) (resp.Value, error) {
	start := time.Now()
	vs := msg.Args[1:]

	var ok bool
	var shaSum string
	var results []int
	var ires int
	for len(vs) > 0 {
		if vs, shaSum, ok = tokenval(vs); !ok || shaSum == "" {
			return NOMessage, errInvalidNumberOfArguments
		}
		_, ok = s.luascripts.Get(shaSum)
		if ok {
			ires = 1
		} else {
			ires = 0
		}
		results = append(results, ires)
	}

	switch msg.OutputType {
	case JSON:
		var buf bytes.Buffer
		buf.WriteString(`{"ok":true`)
		var resArray []string
		for _, ires := range results {
			resArray = append(resArray, fmt.Sprintf("%d", ires))
		}
		buf.WriteString(`,"result":[` + strings.Join(resArray, ",") + `]`)
		buf.WriteString(`,"elapsed":"` + time.Since(start).String() + "\"}")
		return resp.StringValue(buf.String()), nil
	case RESP:
		var resArray []resp.Value
		for _, ires := range results {
			resArray = append(resArray, resp.IntegerValue(ires))
		}
		return resp.ArrayValue(resArray), nil
	}
	return resp.SimpleStringValue(""), nil
}

func (s *Server) cmdScriptFlush(msg *Message) (resp.Value, error) {
	start := time.Now()
	s.luascripts.Flush()

	switch msg.OutputType {
	case JSON:
		var buf bytes.Buffer
		buf.WriteString(`{"ok":true`)
		buf.WriteString(`,"elapsed":"` + time.Since(start).String() + "\"}")
		return resp.StringValue(buf.String()), nil
	case RESP:
		return resp.StringValue("OK"), nil
	}
	return resp.SimpleStringValue(""), nil
}

func (s *Server) commandInScript(msg *Message) (
	res resp.Value, d commandDetails, err error,
) {
	switch msg.Command() {
	default:
		err = fmt.Errorf("unknown command '%s'", msg.Args[0])
	case "set":
		res, d, err = s.cmdSET(msg)
	case "fset":
		res, d, err = s.cmdFSET(msg)
	case "del":
		res, d, err = s.cmdDEL(msg)
	case "pdel":
		res, d, err = s.cmdPDEL(msg)
	case "drop":
		res, d, err = s.cmdDROP(msg)
	case "expire":
		res, d, err = s.cmdEXPIRE(msg)
	case "rename":
		res, d, err = s.cmdRENAME(msg)
	case "renamenx":
		res, d, err = s.cmdRENAME(msg)
	case "persist":
		res, d, err = s.cmdPERSIST(msg)
	case "ttl":
		res, err = s.cmdTTL(msg)
	case "stats":
		res, err = s.cmdSTATS(msg)
	case "scan":
		res, err = s.cmdScan(msg)
	case "nearby":
		res, err = s.cmdNearby(msg)
	case "within":
		res, err = s.cmdWITHIN(msg)
	case "intersects":
		res, err = s.cmdINTERSECTS(msg)
	case "search":
		res, err = s.cmdSearch(msg)
	case "bounds":
		res, err = s.cmdBOUNDS(msg)
	case "get":
		res, err = s.cmdGET(msg)
	case "fget":
		res, err = s.cmdFGET(msg)
	case "jget":
		res, err = s.cmdJget(msg)
	case "jset":
		res, d, err = s.cmdJset(msg)
	case "jdel":
		res, d, err = s.cmdJdel(msg)
	case "type":
		res, err = s.cmdTYPE(msg)
	case "keys":
		res, err = s.cmdKEYS(msg)
	case "exists":
		res, err = s.cmdEXISTS(msg)
	case "fexists":
		res, err = s.cmdFEXISTS(msg)
	case "test":
		res, err = s.cmdTEST(msg)
	case "server":
		res, err = s.cmdSERVER(msg)
	}
	s.sendMonitor(err, msg, nil, true)
	return
}

func (s *Server) luaTile38Call(evalcmd string, cmd string, args ...string) (resp.Value, error) {
	msg := &Message{}
	msg.OutputType = RESP
	msg.Args = append([]string{cmd}, args...)

	if msg.Command() == "timeout" {
		if err := rewriteTimeoutMsg(msg); err != nil {
			return resp.NullValue(), err
		}
	}

	switch msg.Command() {
	case "ping", "echo", "auth", "massinsert", "shutdown", "gc",
		"sethook", "pdelhook", "delhook",
		"follow", "readonly", "config", "output", "client",
		"aofshrink",
		"script load", "script exists", "script flush",
		"eval", "evalsha", "evalro", "evalrosha", "evalna", "evalnasha":
		return resp.NullValue(), errCmdNotSupported
	}

	switch evalcmd {
	case "eval", "evalsha":
		return s.luaTile38AtomicRW(msg)
	case "evalro", "evalrosha":
		return s.luaTile38AtomicRO(msg)
	case "evalna", "evalnasha":
		return s.luaTile38NonAtomic(msg)
	}

	return resp.NullValue(), errCmdNotSupported
}

// The eval command has already got the lock. No locking on the call from within the script.
func (s *Server) luaTile38AtomicRW(msg *Message) (resp.Value, error) {
	var write bool

	switch msg.Command() {
	default:
		return resp.NullValue(), errCmdNotSupported
	case "set", "del", "drop", "fset", "flushdb", "expire", "persist", "jset", "pdel",
		"rename", "renamenx":
		// write operations
		write = true
		if s.config.followHost() != "" {
			return resp.NullValue(), errNotLeader
		}
		if s.config.readOnly() {
			return resp.NullValue(), errReadOnly
		}
	case "get", "keys", "scan", "nearby", "within", "intersects", "hooks", "search",
		"ttl", "bounds", "server", "info", "type", "jget", "fget", "exists", "fexists", "test":
		// read operations
		if s.config.followHost() != "" && !s.fcuponce {
			return resp.NullValue(), errCatchingUp
		}
	}

	res, d, err := func() (res resp.Value, d commandDetails, err error) {
		if msg.Deadline != nil {
			if write {
				res = NOMessage
				err = errTimeoutOnCmd(msg.Command())
				return
			}
			defer func() {
				if msg.Deadline.Hit() {
					v := recover()
					if v != nil {
						if s, ok := v.(string); !ok || s != "deadline" {
							panic(v)
						}
					}
					res = NOMessage
					err = errTimeout
				}
			}()
		}
		return s.commandInScript(msg)
	}()
	if err != nil {
		return resp.NullValue(), err
	}

	if write {
		if err := s.writeAOF(msg.Args, &d); err != nil {
			return resp.NullValue(), err
		}
	}

	return res, nil
}

func (s *Server) luaTile38AtomicRO(msg *Message) (resp.Value, error) {
	switch msg.Command() {
	default:
		return resp.NullValue(), errCmdNotSupported

	case "set", "del", "drop", "fset", "flushdb", "expire", "persist", "jset", "pdel",
		"rename", "renamenx":
		// write operations
		return resp.NullValue(), errReadOnly

	case "get", "keys", "scan", "nearby", "within", "intersects", "hooks", "search",
		"ttl", "bounds", "server", "info", "type", "jget", "fget", "exists", "fexists", "test":
		// read operations
		if s.config.followHost() != "" && !s.fcuponce {
			return resp.NullValue(), errCatchingUp
		}
	}

	res, _, err := func() (res resp.Value, d commandDetails, err error) {
		if msg.Deadline != nil {
			defer func() {
				if msg.Deadline.Hit() {
					v := recover()
					if v != nil {
						if s, ok := v.(string); !ok || s != "deadline" {
							panic(v)
						}
					}
					res = NOMessage
					err = errTimeout
				}
			}()
		}
		return s.commandInScript(msg)
	}()
	if err != nil {
		return resp.NullValue(), err
	}

	return res, nil
}

func (s *Server) luaTile38NonAtomic(msg *Message) (resp.Value, error) {
	var write bool

	// choose the locking strategy
	switch msg.Command() {
	default:
		return resp.NullValue(), errCmdNotSupported
	case "set", "del", "drop", "fset", "flushdb", "expire", "persist", "jset", "pdel",
		"rename", "renamenx":
		// write operations
		write = true
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.config.followHost() != "" {
			return resp.NullValue(), errNotLeader
		}
		if s.config.readOnly() {
			return resp.NullValue(), errReadOnly
		}
	case "get", "keys", "scan", "nearby", "within", "intersects", "hooks", "search",
		"ttl", "bounds", "server", "info", "type", "jget", "fget", "exists", "fexists", "test":
		// read operations
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.config.followHost() != "" && !s.fcuponce {
			return resp.NullValue(), errCatchingUp
		}
	}

	res, d, err := func() (res resp.Value, d commandDetails, err error) {
		if msg.Deadline != nil {
			if write {
				res = NOMessage
				err = errTimeoutOnCmd(msg.Command())
				return
			}
			defer func() {
				if msg.Deadline.Hit() {
					v := recover()
					if v != nil {
						if s, ok := v.(string); !ok || s != "deadline" {
							panic(v)
						}
					}
					res = NOMessage
					err = errTimeout
				}
			}()
		}
		return s.commandInScript(msg)
	}()
	if err != nil {
		return resp.NullValue(), err
	}

	if write {
		if err := s.writeAOF(msg.Args, &d); err != nil {
			return resp.NullValue(), err
		}
	}

	return res, nil
}

// Opens a subset of the Lua 5.1 base module (tonumber, tostring)
func openBaseSubset(L *lua.LState) int {
	basefns := map[string]lua.LGFunction{
		"tonumber": baseToNumber,
		"tostring": baseToString,
	}

	global := L.Get(lua.GlobalsIndex).(*lua.LTable)
	L.SetGlobal("_G", global)
	L.SetGlobal("_VERSION", lua.LString(lua.LuaVersion))
	L.SetGlobal("_GOPHER_LUA_VERSION", lua.LString(lua.PackageName+" "+lua.PackageVersion))
	basemod := L.RegisterModule("_G", basefns)
	L.Push(basemod)
	return 1

}

// Opens a subset of the Lua 5.1 os module (clock, difftime)
func openOsSubset(L *lua.LState) int {
	osfns := map[string]lua.LGFunction{
		"clock":    osClock,
		"difftime": osDiffTime,
	}
	osmod := L.RegisterModule(lua.OsLibName, osfns)
	L.Push(osmod)
	return 1
}

// Lua tonumber()
func baseToNumber(L *lua.LState) int {
	base := L.OptInt(2, 10)
	noBase := L.Get(2) == lua.LNil

	switch lv := L.CheckAny(1).(type) {
	case lua.LNumber:
		L.Push(lv)
	case lua.LString:
		str := strings.Trim(string(lv), " \n\t")
		if strings.Contains(str, ".") {
			if v, err := strconv.ParseFloat(str, lua.LNumberBit); err != nil {
				L.Push(lua.LNil)
			} else {
				L.Push(lua.LNumber(v))
			}
		} else {
			if noBase && strings.HasPrefix(strings.ToLower(str), "0x") {
				base, str = 16, str[2:] // Hex number
			}
			if v, err := strconv.ParseInt(str, base, lua.LNumberBit); err != nil {
				L.Push(lua.LNil)
			} else {
				L.Push(lua.LNumber(v))
			}
		}
	default:
		L.Push(lua.LNil)
	}
	return 1
}

// Lua tostring()
func baseToString(L *lua.LState) int {
	v1 := L.CheckAny(1)
	L.Push(L.ToStringMeta(v1))
	return 1
}

// Lua os.clock()
func osClock(L *lua.LState) int {
	L.Push(lua.LNumber(time.Since(startedAt).Seconds()))
	return 1
}

// Lua os.difftime()
func osDiffTime(L *lua.LState) int {
	L.Push(lua.LNumber(L.CheckInt64(1) - L.CheckInt64(2)))
	return 1
}
