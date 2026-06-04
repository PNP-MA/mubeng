package server

import (
	"io"
	"net"
	"sync"
)

// MonitoredConn wraps a net.Conn and automatically removes the associated
// proxy from the pool if a transport-level error occurs (e.g. connection
// reset, TLS failure) after the proxy has already accepted the CONNECT tunnel.
//
// This catches the case where a proxy returns 200 OK to CONNECT but then
// MITMs TLS or drops the connection — errors invisible to the dial stage.
type MonitoredConn struct {
	net.Conn
	proxyAddr string
	onRemove  func(string)
	once      sync.Once
}

func (m *MonitoredConn) Read(b []byte) (int, error) {
	n, err := m.Conn.Read(b)
	if err != nil && err != io.EOF {
		m.once.Do(func() {
			m.onRemove(m.proxyAddr)
		})
	}
	return n, err
}

func (m *MonitoredConn) Write(b []byte) (int, error) {
	n, err := m.Conn.Write(b)
	if err != nil {
		m.once.Do(func() {
			m.onRemove(m.proxyAddr)
		})
	}
	return n, err
}
