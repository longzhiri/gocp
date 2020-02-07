# gocp [![Doc](https://img.shields.io/badge/go-doc-blue.svg)](https://godoc.org/github.com/longzhiri/gocp) [![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/longzhiri/gocp/blob/master/LICENSE)
A connection pool implementation  in **Go**

# Install
```bash
go get -u github.com/longzhiri/gocp
```

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