package gocp

import (
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"context"
	_ "net/http/pprof"
)

var testServerStarted bool

func setupTestServer(network, address string) {
	if testServerStarted {
		return
	}
	testServerStarted = true
	ln, err := net.Listen(network, address)
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				log.Fatalf("accept failed: %v", err)
			}
			go func() {
				var buf [1]byte
				conn.Read(buf[:])
				conn.Close()
			}()
		}
	}()
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}

func concurSimulate(ctx context.Context, cp *ConnPool, concurrency int, maxLoopCount int, minLoopCount int, connOptSetter func(conn net.Conn), reuse func() bool) {
	log.Printf("concurSimulate concurrency=%v maxLoopCount=%v minLoopCount=%v", concurrency, maxLoopCount, minLoopCount)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(gid int) {
			loopCount := int(rand.Int31n(int32(maxLoopCount-minLoopCount+1))) + minLoopCount
			for j := 0; j < loopCount; j++ {
				conn, err := cp.Get(ctx)
				if err != nil {
					log.Printf("Get failed: %v", err)
					break
				}
				if connOptSetter != nil {
					connOptSetter(conn)
				}
				time.Sleep(time.Duration(rand.Int63n(1500)) * time.Millisecond)
				cp.Free(conn, reuse())
			}
			//log.Printf("goroutine=%v finished", gid)
			wg.Done()
		}(i)
	}
	quitC := make(chan struct{})
	go func() {
		for q := false; !q; {
			select {
			case <-time.After(3 * time.Second):
				if !cp.assert() {
					log.Fatalf("cp assert failed")
				}
				co, cf, cr := cp.Stats()
				log.Printf("curOpenNum=%v curFreeNum=%v curReqNum=%v", co, cf, cr)
			case <-quitC:
				q = true
			}
		}
	}()
	wg.Wait()
	close(quitC)
}

func TestConnPoolMaxOpenNum(t *testing.T) {
	rand.Seed(time.Now().Unix())
	setupTestServer("tcp", ":12347")

	cp := NewConnPool("tcp", ":12347", 10, 0, 0)
	var conns []net.Conn
	for i := 0; i < 10; i++ {
		conn, _ := cp.Get(context.TODO())
		conns = append(conns, conn)
	}
	ctx, cancler := context.WithDeadline(context.TODO(), time.Now().Add(time.Millisecond))
	_, err := cp.Get(ctx)
	if err == nil {
		t.Fatalf("Get not blocked when the number of open connection reached limit")
	}
	cancler()
	for _, conn := range conns {
		cp.Free(conn, true)
	}
	ctx, cancler = context.WithDeadline(context.TODO(), time.Now().Add(time.Millisecond))
	conn, err := cp.Get(ctx)
	if err != nil {
		t.Fatalf("Get blocked when the number of open connection doesn't reach limit")
	}
	cancler()
	cp.Free(conn, true)
	cp.Close()

	cp = NewConnPool("tcp", ":12347", 256, 0, 0)
	concurSimulate(context.TODO(), cp, 1024, 50, 10, func(conn net.Conn) {
		tcpConn, ok := conn.(*net.TCPConn)
		if ok {
			tcpConn.SetLinger(0)
		}
	}, func() bool {
		return rand.Intn(2) == 1
	})
	if !cp.assert() {
		t.Fatalf("cp assert failed")
	}
	cp.Close()
}

func TestConnPoolMaxIdleNum(t *testing.T) {
	rand.Seed(time.Now().Unix())
	setupTestServer("tcp", ":12347")

	cp := NewConnPool("tcp", ":12347", 0, 1024, 0)
	for j := 0; j < 10; j++ {
		var conns []net.Conn
		for i := 0; i < 10; i++ {
			conn, _ := cp.Get(context.TODO())
			tcpConn, ok := conn.(*net.TCPConn)
			if ok {
				tcpConn.SetLinger(0)
			}
			conns = append(conns, conn)
		}
		var reusedNum int
		for _, conn := range conns {
			if rand.Int31n(2) == 1 {
				cp.Free(conn, true)
				reusedNum++
			} else {
				cp.Free(conn, false)
			}
		}
		_, cf, _ := cp.Stats()
		if cf != reusedNum {
			t.Fatalf("free conns should be %v, but %v return", reusedNum, cf)
		}
	}
	cp.Close()

	cp = NewConnPool("tcp", ":12347", 0, 1024, 0)
	concurSimulate(context.TODO(), cp, 256, 50, 10, func(conn net.Conn) {
		tcpConn, ok := conn.(*net.TCPConn)
		if ok {
			tcpConn.SetLinger(0)
		}
	}, func() bool { return true })
	_, cf, _ := cp.Stats()
	if cf != 256 {
		t.Fatalf("free conns should be %v, but %v return", 256, cf)
	}
	if !cp.assert() {
		t.Fatalf("cp assert failed")
	}
	cp.Close()

	cp = NewConnPool("tcp", ":12347", 0, 512, 0)
	concurSimulate(context.TODO(), cp, 1024, 50, 10, func(conn net.Conn) {
		tcpConn, ok := conn.(*net.TCPConn)
		if ok {
			tcpConn.SetLinger(0)
		}
	}, func() bool { return true })
	_, cf, _ = cp.Stats()
	if cf != 512 {
		t.Fatalf("free conns should be %v, but %v return", 512, cf)
	}
	if !cp.assert() {
		t.Fatalf("cp assert failed")
	}
	cp.Close()
}

func TestCoonPoolMaxLifeTime(t *testing.T) {
	rand.Seed(time.Now().Unix())
	setupTestServer("tcp", ":12347")

	cp := NewConnPool("tcp", ":12347", 0, 1, time.Millisecond)

	conn1, _ := cp.Get(context.TODO())
	cp.Free(conn1, true)
	conn2, _ := cp.Get(context.TODO())
	if conn1 != conn2 {
		t.Fatalf("unexpired connection reused failed")
	}
	cp.Free(conn2, true)

	time.Sleep(time.Millisecond)

	conn3, _ := cp.Get(context.TODO())
	if conn2 == conn3 {
		t.Fatalf("expired connection reused")
	}

	time.Sleep(time.Millisecond)
	cp.Free(conn3, true)

	_, fc, _ := cp.Stats()
	if fc != 0 {
		t.Fatalf("unexpired connection has't been reused")
	}

	cp.Close()

	cp = NewConnPool("tcp", ":12347", 0, 256, time.Millisecond)
	concurSimulate(context.TODO(), cp, 256, 50, 10, func(conn net.Conn) {
		tcpConn, ok := conn.(*net.TCPConn)
		if ok {
			tcpConn.SetLinger(0)
		}
	}, func() bool { return true })
	if !cp.assert() {
		t.Fatalf("cp assert failed")
	}
	cp.Close()
}

func TestConnPoolMix(t *testing.T) {
	rand.Seed(time.Now().Unix())
	setupTestServer("tcp", ":12347")

	cp := NewConnPool("tcp", ":12347", 1024, 251, 5*time.Second)
	concurSimulate(context.TODO(), cp, 1200, 80, 20, func(conn net.Conn) {
		tcpConn, ok := conn.(*net.TCPConn)
		if ok {
			tcpConn.SetLinger(0)
		}
	}, func() bool {
		return rand.Intn(2) == 1
	})
	if !cp.assert() {
		t.Fatalf("cp assert failed")
	}
	cp.Close()
}

var benchCP *ConnPool

func BenchmarkConnPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		conn, _ := benchCP.Get(context.TODO())
		benchCP.Free(conn, true)
	}
	log.Printf("finished")
}

func TestMain(m *testing.M) {
	rand.Seed(time.Now().Unix())
	setupTestServer("tcp", ":12347")
	benchCP = NewConnPool("tcp", ":12347", 2000, 1300, 5*time.Second)

	go concurSimulate(context.TODO(), benchCP, 1200, 1000, 100, func(conn net.Conn) {
		tcpConn, ok := conn.(*net.TCPConn)
		if ok {
			tcpConn.SetLinger(0)
		}
	}, func() bool {
		return true
	})
	// warm up
	conn, _ := benchCP.Get(context.TODO())
	tcpConn, ok := conn.(*net.TCPConn)
	if ok {
		tcpConn.SetLinger(0)
	}
	benchCP.Free(conn, true)
	os.Exit(m.Run())
}
