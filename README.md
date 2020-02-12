# gocp
[![Doc](https://img.shields.io/badge/go-doc-blue.svg)](https://godoc.org/github.com/longzhiri/gocp)
[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/longzhiri/gocp/blob/master/LICENSE)
[![codecov](https://codecov.io/gh/longzhiri/gocp/branch/master/graph/badge.svg)](https://codecov.io/gh/longzhiri/gocp)
[![Build Status](https://travis-ci.org/longzhiri/gocp.svg?branch=master)](https://travis-ci.org/longzhiri/gocp)

A connection pool implementation  in **Go**

# Install
```bash
go get -u github.com/longzhiri/gocp
```

# Introduce
The connection pool refers to [go-database-sql](https://golang.org/pkg/database/sql/) and has three configuration items:
    
    The maxOpenNum is maximum number of open connections by pool, if <=0, then there is no limit 
    for open connections.
    
    The maxIdleNum is maximum number of idle connections in pool, if <=0, no idle connections are retained. 
    If maxOpenNum is not 0 and maxIdleNum is greater than maxOpenNum, then maxIdleNum will be reduced to 
    match maxOpenNum limit.
    
    The maxLifeTime is maximum amount of time a connection may be reused, if <=0, connections are reused forever. 
    Expired connection will be lazily closed when try reusing. 

# Usage
See [godoc](https://godoc.org/github.com/longzhiri/gocp) in details.
```go
import "github.com/longzhiri/gocp"

// Instantiate a connection pool
cp := gocp.NewConnPool("tcp", ":12347", 1024, 256, 30*time.Second)
// Get a connection from the pool
conn, err := cp.Get(context.TODO())
if err != nil {
    return err
}
// Do something with conn

// Free this connection
cp.Free(conn, false) // or cp.Free(conn, true) if you want to reuse this connection
```

# License
The MIT License (MIT)