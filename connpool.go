// Package gocp provide a configurable connection pool, and can be accessed by multiple goroutines simultaneously.
package gocp

import (
	"context"
	"net"
	"sync"
	"time"

	"errors"
)

var (
	ErrOperationTerm  = errors.New("operation was terminated")
	ErrConnPoolClosed = errors.New("conn pool was already closed")
)

// ConnPool represents a connection pool.
//
// Example usage:
//  cp := NewConnPool("tcp", ":12347", 1024, 251, 5*time.Second)
//  conn, err := cp.Get(context.TODO())
//  if err == nil {
//      // do something
//      cp.Free(conn, true)
//  }
type ConnPool struct {
	network string
	address string

	maxOpenNum  int
	maxIdleNum  int
	maxLifeTime time.Duration

	mu        sync.Mutex
	freeConns []net.Conn

	curOpenNum int

	connRequests       map[uint64]chan net.Conn
	nextConnRequestKey uint64

	connsExpireTime map[net.Conn]int64

	closed bool
}

// NewConnPool instantiate a connection pool.
//
// The network and the address are the same as net.Dial.
//
// The maxOpenNum is maximum number of open connections by pool,
// if <=0, then there is no limit for open connections.
//
// The maxIdleNum is maximum number of idle connections in pool,
// if <=0, no idle connections are retained.
// If maxOpenNum is not 0 and maxIdleNum is greater than maxOpenNum, then maxIdleNum will be reduced to match maxOpenNum limit.
//
// The maxLifeTime is maximum amount of time a connection may be reused,
// if <=0, connections are reused forever.
// Expired connection will be lazily closed when try reusing.
func NewConnPool(network, address string, maxOpenNum int, maxIdleNum int, maxLifeTime time.Duration) *ConnPool {
	if maxOpenNum != 0 && maxIdleNum > maxOpenNum {
		maxIdleNum = maxOpenNum
	}
	return &ConnPool{
		network: network,
		address: address,

		maxOpenNum:  maxOpenNum,
		maxIdleNum:  maxIdleNum,
		maxLifeTime: maxLifeTime,

		connsExpireTime: make(map[net.Conn]int64),

		connRequests: make(map[uint64]chan net.Conn),
	}
}

// Get returns a connection from the pool, either by reusing a idle connection if exists or by creating a new one.
//
// If no any idle connections exist and the number of open connections has already reached the upper limit, the Get will be blocked until a connection is closed or reused.
//
// If the pool is closed, Get fails with ErrConnPoolClosed.
// If the ctx is cancelled by caller, Get fails with ErrConnPoolClosed.
func (p *ConnPool) Get(ctx context.Context) (net.Conn, error) {
	p.mu.Lock()
	if p.closed {
		return nil, ErrConnPoolClosed
	}
	var conn net.Conn
	for len(p.freeConns) > 0 {
		conn = p.freeConns[0]
		p.freeConns = p.freeConns[1:]

		expireTime := p.connsExpireTime[conn]
		if expireTime == 0 || expireTime > time.Now().UnixNano() {
			break
		}
		p.closeConnLocked(conn)
		conn = nil
	}
	if conn != nil {
		p.mu.Unlock()
		return conn, nil
	}
	if p.maxOpenNum != 0 && p.curOpenNum >= p.maxOpenNum {
		req := make(chan net.Conn, 1)
		p.nextConnRequestKey++
		connReqKey := p.nextConnRequestKey
		p.connRequests[connReqKey] = req
		p.mu.Unlock()
		select {
		case c := <-req:
			if c == nil { // a connection closed, try Get again.
				return p.Get(ctx)
			}
			return c, nil
		case <-ctx.Done():
			p.mu.Lock()
			delete(p.connRequests, connReqKey)
			p.mu.Unlock()

			// may a connection response has already been sent, reuse it.
			select {
			case c := <-req:
				if c != nil {
					p.Free(c, true)
				}
			default:
			}
			return nil, ErrOperationTerm
		}
	} else {
		p.curOpenNum++
		p.mu.Unlock()

		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, p.network, p.address)
		if err != nil {
			p.mu.Lock()
			p.curOpenNum--
			for rk, rc := range p.connRequests {
				delete(p.connRequests, rk)
				rc <- nil
				break
			}
			p.mu.Unlock()
		} else {
			p.mu.Lock()
			if p.maxLifeTime > 0 {
				p.connsExpireTime[conn] = time.Now().Add(p.maxLifeTime).UnixNano()
			}
			p.mu.Unlock()
		}
		return conn, err
	}
}

// Free frees a connection which was got from the pool.
//
// If the reusable is true, the connection will be reused immediately by waking a Get calling up or be put into idle queue,
// or it will be closed and detached from the pool.
func (p *ConnPool) Free(conn net.Conn, reusable bool) {
	if !reusable {
		p.mu.Lock()
		p.closeConnLocked(conn)
		p.mu.Unlock()
	} else {
		p.mu.Lock()
		if p.closed {
			p.closeConnLocked(conn)
			p.mu.Unlock()
			return
		}

		expireTime := p.connsExpireTime[conn]
		if expireTime != 0 && expireTime <= time.Now().UnixNano() {
			p.closeConnLocked(conn)
			p.mu.Unlock()
			return
		}

		for rk, rc := range p.connRequests {
			delete(p.connRequests, rk)
			rc <- conn
			p.mu.Unlock()
			return
		}

		if len(p.freeConns) < p.maxIdleNum {
			p.freeConns = append(p.freeConns, conn)
		} else {
			p.closeConnLocked(conn)
		}
		p.mu.Unlock()
	}
}

func (p *ConnPool) closeConnLocked(conn net.Conn) {
	conn.Close()
	p.curOpenNum--
	delete(p.connsExpireTime, conn)
	if !p.closed {
		for rk, rc := range p.connRequests {
			delete(p.connRequests, rk)
			rc <- nil
			break
		}
	}
}

// Close marks the pool as closed, frees all idle connections, and wakes all blocked Get callings up.
func (p *ConnPool) Close() {
	p.mu.Lock()

	for _, conn := range p.freeConns {
		conn.Close()
	}
	p.freeConns = nil

	connRequests := p.connRequests
	p.connRequests = make(map[uint64]chan net.Conn)
	p.closed = true
	p.mu.Unlock()
	for _, rc := range connRequests {
		rc <- nil
	}
}

// Stats returns the pool statistics.
func (p *ConnPool) Stats() (curOpenNum int, curFreeNum int, curReqNum int) {
	p.mu.Lock()
	curOpenNum, curFreeNum, curReqNum = p.curOpenNum, len(p.freeConns), len(p.connRequests)
	p.mu.Unlock()
	return
}

// assert asserts the pool status.
func (p *ConnPool) assert() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.maxOpenNum != 0 && p.curOpenNum > p.maxOpenNum {
		return false
	}

	if len(p.freeConns) > p.maxIdleNum {
		return false
	}

	return true
}
