package ssh

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

type ClientRouterCallback func(*ServerConn) (string, net.Conn, *ClientConfig, error)

func Proxy(usNetConn net.Conn, usConfig *ServerConfig, cb ClientRouterCallback) error {
	usFull, err := serverFullConfig(usConfig)
	if err != nil {
		usNetConn.Close()
		return err
	}

	us := &connection{
		sshConn: sshConn{conn: usNetConn},
	}
	perms, err := us.serverHandshakeNoMux(usFull)
	if err != nil {
		usNetConn.Close()
		return err
	}

	dsAddr, dsNetConn, dsConfig, err := cb(&ServerConn{us, perms})
	if err != nil {
		usNetConn.Close()
		return err
	}

	dsFull, err := clientFullConfig(dsConfig)
	if err != nil {
		usNetConn.Close()
		dsNetConn.Close()
		return err
	}

	ds := &connection{
		sshConn: sshConn{conn: dsNetConn, user: dsFull.User},
	}

	if err := ds.clientHandshake(dsAddr, dsFull); err != nil {
		usNetConn.Close()
		dsNetConn.Close()
		return fmt.Errorf("ssh: handshake failed: %w", err)
	}

	var (
		wg         sync.WaitGroup
		errA, errB error
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		errA = proxyConnection(us, ds)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errB = proxyConnection(ds, us)
	}()

	wg.Wait()

	return errors.Join(errA, errB)
}

func proxyConnection(src, dst *connection) error {
	defer dst.Close()
	defer src.Close()

	for {
		p, err := src.transport.readPacket()
		if err != nil {
			return err
		}

		if err := dst.transport.writePacket(p); err != nil {
			return err
		}
	}
}
