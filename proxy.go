package main

import (
	"grogon/providers"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const IDLE_TIMEOUT = 10 * time.Minute
const DIAL_TIMEOUT = 2 * time.Second

var bufferPool = sync.Pool{
	New: func() interface{} {
		buffer := make([]byte, 32*1024)
		return &buffer
	},
}

type proxyMetrics struct {
	current_active_connections     prometheus.Gauge
	total_connections_handled      prometheus.Counter
	total_connection_failures      *prometheus.CounterVec
	connection_duration_seconds    prometheus.Histogram
	upstream_dial_duration_seconds prometheus.Histogram
	bytes_transferred              prometheus.Counter
}

func NewMetrics(reg prometheus.Registerer) *proxyMetrics {
	metrics := &proxyMetrics{
		current_active_connections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "current_active_connections",
			Help: "Current Active Connections",
		}),
		total_connections_handled: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "total_connections_handled",
				Help: "Total Number of Connections Handled",
			},
		),
		total_connection_failures: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "total_connection_failures",
				Help: "Total Number of Connections Failures.",
			},
			[]string{"reason"},
		),
		connection_duration_seconds: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "connection_duration_seconds",
				Help:    "Duration of connections in seconds.",
				Buckets: prometheus.LinearBuckets(0.1, 0.1, 20),
			},
		),
		upstream_dial_duration_seconds: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "upstream_dial_duration_seconds",
				Help:    "Time taken to dial upstream server",
				Buckets: []float64{0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.5},
			},
		),
		bytes_transferred: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "bytes_transferred_total",
				Help: "Total bytes transferred",
			},
		),
	}
	reg.MustRegister(metrics.current_active_connections)
	reg.MustRegister(metrics.total_connections_handled)
	reg.MustRegister(metrics.total_connection_failures)
	reg.MustRegister(metrics.connection_duration_seconds)
	reg.MustRegister(metrics.upstream_dial_duration_seconds)
	reg.MustRegister(metrics.bytes_transferred)
	return metrics
}

type IServerProvider interface {
	Next() string
}

func main() {
	reg := prometheus.NewRegistry()
	metrics := NewMetrics(reg)

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	go func() {
		log.Println("Metrics server listening on :8081")
		if err := http.ListenAndServe(":8081", nil); err != nil {
			log.Fatalf("Metrics server error: %v", err)
		}
	}()

	go func() {
		log.Println("Starting pprof server on :6060")
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	upstreams := []string{
		"upstream1:7777",
		"upstream2:7777",
	}

	var serverProvider = providers.NewRRServerProvider(upstreams)
	var wg sync.WaitGroup

	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatalf("Failed to start listener: %v", err)
	}
	defer listener.Close()

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigChan
		log.Printf("Received signal: %v. Shutting down...", sig)
		listener.Close()
	}()

	log.Printf("Gorgon TCP Proxy listening on :8080\n")

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept connection: %v", err)
			break
		}

		wg.Add(1)
		go handleConnection(clientConn, serverProvider, &wg, metrics)
	}
	wg.Wait()
}

func handleConnection(clientConn net.Conn, provider IServerProvider, wg *sync.WaitGroup, metrics *proxyMetrics) {
	defer wg.Done()
	upstreamServer := provider.Next()
	if upstreamServer == "" {
		log.Println("No healthy upstream servers available.")
		clientConn.Close()
		return
	}

	log.Printf("Handling connection from %s, proxying to %s\n", clientConn.RemoteAddr(), upstreamServer)
	metrics.current_active_connections.Inc()
	defer metrics.current_active_connections.Dec()

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		metrics.connection_duration_seconds.Observe(duration.Seconds())
	}()

	metrics.total_connections_handled.Inc()

	dialStart := time.Now()
	serverConn, err := net.DialTimeout("tcp", upstreamServer, DIAL_TIMEOUT)
	metrics.upstream_dial_duration_seconds.Observe(time.Since(dialStart).Seconds())

	if err != nil {
		metrics.total_connection_failures.WithLabelValues("dial_error").Inc()
		log.Printf("Failed to dial upstream '%s': %v", upstreamServer, err)
		clientConn.Close()
		return
	}

	defer clientConn.Close()
	defer serverConn.Close()

	go copyWithIdleTimeout(clientConn, serverConn, IDLE_TIMEOUT, metrics)
	copyWithIdleTimeout(serverConn, clientConn, IDLE_TIMEOUT, metrics)

	log.Printf("Closing connection from %s (upstream %s)\n", clientConn.RemoteAddr(), upstreamServer)
}

func copyWithIdleTimeout(destination net.Conn, source net.Conn, timeout time.Duration, metrics *proxyMetrics) {
	bufferPtr := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(bufferPtr)
	buffer := *bufferPtr

	for {
		source.SetReadDeadline(time.Now().Add(timeout))

		numBytes, err := source.Read(buffer)
		if numBytes > 0 {
			metrics.bytes_transferred.Add(float64(numBytes))
		}
		if err != nil {
			if err != io.EOF {
			}
			return
		}

		destination.SetWriteDeadline(time.Now().Add(timeout))
		_, err = destination.Write(buffer[:numBytes])
		if err != nil {
			log.Printf("Write error: %v", err)
			return
		}
	}
}
