package main

import (
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	CONCURRENT_CLIENTS = 50
	TEST_DURATION      = 5 * time.Minute
	TARGET             = "gorgon-proxy:8080"
)

func main() {
	var wg sync.WaitGroup
	var ops uint64 = 0

	log.Printf("Starting Load Test against %s with %d clients...", TARGET, CONCURRENT_CLIENTS)

	deadline := time.Now().Add(TEST_DURATION)

	for i := 0; i < CONCURRENT_CLIENTS; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			proxyConn, err := net.DialTimeout("tcp", TARGET, 5*time.Second)
			if err != nil {
				log.Printf("Client %d: Connect Failed: %v", id, err)
				return
			}
			defer proxyConn.Close()

			payload := []byte("Hello Gorgon!")
			readBuf := make([]byte, 1024)

			for time.Now().Before(deadline) {
				_, err := proxyConn.Write(payload)
				if err != nil {
					log.Printf("Client %d: Write Error: %v", id, err)
					return
				}

				_, err = proxyConn.Read(readBuf)
				if err != nil {
					log.Printf("Client %d: Read Error: %v", id, err)
					return
				}

				atomic.AddUint64(&ops, 1)
			}
		}(i)
	}

	go func() {
		var lastOps uint64 = 0
		for time.Now().Before(deadline) {
			time.Sleep(1 * time.Second)
			currentOps := atomic.LoadUint64(&ops)
			log.Printf("Current Throughput: %d QPS", currentOps-lastOps)
			lastOps = currentOps
		}
	}()

	wg.Wait()
	log.Println("Load Test Finished.")
}
