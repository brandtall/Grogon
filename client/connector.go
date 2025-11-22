package main

import (
	"log"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	CONCURRENT_CLIENTS = 50
	TEST_DURATION      = 5 * time.Minute
	TARGET             = "gorgon-proxy:8080"
)

func main() {
	var wg sync.WaitGroup
	latencyChan := make(chan time.Duration, 100000)

	log.Printf("Starting Latency & Load Test against %s...", TARGET)

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
				reqStart := time.Now()

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

				select {
				case latencyChan <- time.Since(reqStart):
				default:
				}
			}
		}(i)
	}

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		samples := make([]time.Duration, 0, 80000)

		for {
			select {
			case d := <-latencyChan:
				samples = append(samples, d)

			case <-ticker.C:
				count := len(samples)
				if count == 0 {
					continue
				}

				sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

				p99Index := int(float64(count) * 0.99)
				p99 := samples[p99Index]
				p50 := samples[int(float64(count)*0.50)]

				log.Printf("QPS: %d | P50: %v | P99: %v", count, p50, p99)

				samples = samples[:0]
			case <-time.After(TEST_DURATION + 1*time.Second):
				return
			}
		}
	}()

	wg.Wait()
	log.Println("Load Test Finished.")
}
