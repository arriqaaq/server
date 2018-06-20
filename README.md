# server
Simple wrapper around Golang net/http to start/shut a server

This is a custom wrapper around net/http server which handles serving requests and shut down on the required SYSCALL without having to explicitly write code to handle interrupts to shutdown a server. Also implements the basic SO_REUSEPORT functionality to fork multiple child processes to listen to the same port, and act as multiple workers behind the same port.

Features
--------
- Create a simple http server
- Simple interface. One function `Serve` which handles OS interrupts too, to shut down a server
- Used in production environments

Installing
----------

```
go get -u github.com/arriqaaq/server
```

Example
-------

Here's a full example of a Server that listens:

You can run this example from a terminal:

```sh
go run example/main.go
```

```go
package main

import (
	"fmt"
	glok "github.com/arriqaaq/server"
	"log"
	"net/http"
	"os"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "Hello from %v!\n", os.Getpid())
	})
	server := glok.NewServer(mux, "0.0.0.0", 8080)
	err := server.Serve()
	log.Println("terminated", os.Getpid(), err)
}

```

TODO
--------
- Add more test cases


References
--------
- Linux System Programming/Programming linux sockets in C (for info on SO_REUSEPORT)
- https://gravitational.com/blog/golang-ssh-bastion-graceful-restarts/ (implementation of graceful restart based on SO_REUSEPORT)

Contact
-------
Farhan [@arriqaaq](http://twitter.com/arriqaaq)

License
-------
Server is available under the MIT [License](/LICENSE).
