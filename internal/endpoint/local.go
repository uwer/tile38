package endpoint

// LocalPublisher is used to publish local notifications
type LocalPublisher interface {
	Publish(channel string, message ...string) int
}

// LocalConn is an endpoint connection
type LocalConn struct {
	ep        Endpoint
	publisher LocalPublisher
}

func newLocalConn(ep Endpoint, publisher LocalPublisher) *LocalConn {
	return &LocalConn{
		ep:        ep,
		publisher: publisher,
	}
}

// Expired returns true if the connection has expired
func (conn *LocalConn) Expired() bool {
	return false
}

// ExpireNow forces the connection to expire
func (conn *LocalConn) ExpireNow() {
}

// Send sends a message
func (conn *LocalConn) Send(msg string) error {
	conn.publisher.Publish(conn.ep.Local.Channel, msg)
	return nil
}
