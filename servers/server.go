package main

import (
	"io"
	"log"
	"net"
)

func main() {
	listener, err := net.Listen("tcp", ":7777")
	if err != nil {
		log.Fatalf("Error Listening: %v", err)
	}
	defer listener.Close()

	log.Println("Echo Server 1 listening on :7777")

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Error Accepting Client Connection: %v", err)
			continue
		}

		log.Printf("Accepted connection from %s", clientConn.RemoteAddr())

		go handleConnection(clientConn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	_, err := io.Copy(conn, conn)
	
	if err != nil {
		log.Printf("Connection closed or error: %v", err)
	} else {
		log.Println("Connection closed cleanly.")
	}
}
