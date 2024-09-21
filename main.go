package main

import (
	// "fmt"
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/prometheus/client_golang/prometheus/collectors"
    versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

const DEFAULT_VIDEOHUB_PORT = "9990"
const DEFAULT_NAMESPACE = "videohub"
const TCP_TIMEOUT = 2           // Timeout for net.Dial to connect to Videohub (in seconds)
const READ_TIMEOUT = "100ms"    // Since the connection is persistent. we need to define a read timeout to know that the Videohub sent all it's data
const DEFAULT_HTTP_PORT = ":9990"

var ( // config flags
	webConfig    = webflag.AddFlags(kingpin.CommandLine, DEFAULT_HTTP_PORT)
	namespace    = kingpin.Flag("videohub.namespace", "The namespace to use in the exported metrics (ie: <namespace>_device_info ...)").Default(DEFAULT_NAMESPACE).String()
	videohubPort = kingpin.Flag("videohub.port", "The videohub TCP port (Very unlikely to be changed)").Default(DEFAULT_VIDEOHUB_PORT).String()
    readTimeout  = kingpin.Flag("videohub.timeout", "The timeout for reading the data sent from a videohub").Default(READ_TIMEOUT).Duration()
)
var logger *slog.Logger
var versionCollector prometheus.Collector

type VideoHub struct {
	Version              string
	ModelName            string
	FriendlyName         string
	NumInputs            int
	NumOutputs           int
	NumProcessingUnits   int
	NumMonitoringOutputs int
	InputLabels          []string
	OutputLabels         []string
	Routing              []int
	Locks                []string
}

// Enum for the different categories returned by videohub
const (
	PROTOCOL_PREAMBULE = iota + 1
	DEVICE_INFO
	INPUT_LABELS
	OUTPUT_LABELS
	ROUTING
	LOCKS
)

var Categories = map[string]int{
	"PROTOCOL PREAMBLE:":    PROTOCOL_PREAMBULE,
	"VIDEOHUB DEVICE:":      DEVICE_INFO,
	"INPUT LABELS:":         INPUT_LABELS,
	"OUTPUT LABELS:":        OUTPUT_LABELS,
	"VIDEO OUTPUT ROUTING:": ROUTING,
	"VIDEO OUTPUT LOCKS:":   LOCKS,
}

type metrics struct {
	scapeSuccess   prometheus.Gauge
	scrapeDuration prometheus.Gauge
	info           *prometheus.GaugeVec
	numInputs      prometheus.Gauge
	numOutputs     prometheus.Gauge
	inputLabels    *prometheus.GaugeVec
	outputLabels   *prometheus.GaugeVec
	routing        *prometheus.GaugeVec
}

func main() {

	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print("videohub_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger = promslog.New(promslogConfig)
    versionCollector = versioncollector.NewCollector("videohub_exporter")
    prometheus.MustRegister(versionCollector) // register here for default registry
    prometheus.MustRegister(collectors.NewBuildInfoCollector())

	logger.Info("Starting videohub_exporter", "version", version.Info())
	logger.Info("", "build_context", version.BuildContext())

	http.Handle("/metrics", promhttp.Handler()) // Regular metrics endpoint for local exporter metrics. (GoCollector, ProcessCollector, VersionCollector)
	http.HandleFunc("/scrape", ScrapeVideohub)  // Endpoint to do videohub scrapes.
	http.HandleFunc("/-/healthy", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Healthy"))
	})

	srv := &http.Server{}
	srvc := make(chan struct{})
	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
			logger.Error("msg", "Error starting HTTP server", "err", err)
			close(srvc)
		}
	}()

	for {
		select {
		case <-term:
			logger.Info("msg", "Received SIGTERM, exiting gracefully...")
			os.Exit(0)
		case <-srvc:
			os.Exit(1)
		}
	}
}

func ScrapeVideohub(w http.ResponseWriter, r *http.Request) {

	params := r.URL.Query()
	target := params.Get("target")
	if target == "" {
		http.Error(w, "Target parameter is missing", http.StatusBadRequest)
		return
	}

	start := time.Now()
	registry := prometheus.NewRegistry()
    registry.MustRegister(versionCollector)
	metrics := NewMetrics(registry)

	logger.Info("Scrapping videohub", "target", target)
	videohub, err := ParseVideohub(target)
	if err != nil {
		metrics.scapeSuccess.Set(0)
		logger.Error("Error scrapping videohub", "target", target, "err", err)
	} else {
		metrics.scapeSuccess.Set(1)
		logger.Debug("Generating metrics", "target", target)
		SetVideoHubMetrics(videohub, metrics)
	}

	duration := time.Since(start).Seconds()
	metrics.scrapeDuration.Set(duration)

	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}

func ParseVideohub(ip string) (VideoHub, error) {

	videohub := VideoHub{}
	category := 0
	addr := net.JoinHostPort(ip, *videohubPort)

	conn, err := net.DialTimeout("tcp", addr, TCP_TIMEOUT*time.Second)
	if err != nil {
		return videohub, err
	}
	conn.SetReadDeadline(time.Now().Add(*readTimeout))

	scanner := bufio.NewScanner(conn)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		// fmt.Println(line)

		// Select category
		new_category, exists := Categories[line]
		if exists {
			category = new_category
			continue
		}

		// Parse each category data accordingly
		switch category {
		case PROTOCOL_PREAMBULE, DEVICE_INFO:
			words := strings.Split(line, ": ")
			if len(words) < 2 {
				break
			}
			key := words[0]
			value := words[1]
			switch key {
			case "Version":
				videohub.Version = strings.Trim(value, " ")
			case "Model name":
				videohub.ModelName = value
			case "Friendly name":
				videohub.FriendlyName = value
			case "Video inputs":
				videohub.NumInputs, _ = strconv.Atoi(value)
				videohub.InputLabels = make([]string, videohub.NumInputs)
			case "Video outputs":
				videohub.NumOutputs, _ = strconv.Atoi(value)
				videohub.OutputLabels = make([]string, videohub.NumOutputs)
				videohub.Routing = make([]int, videohub.NumOutputs)
				videohub.Locks = make([]string, videohub.NumOutputs)
			case "Video processing units":
				videohub.NumProcessingUnits, _ = strconv.Atoi(value)
			case "Video monitoring outputs":
				videohub.NumMonitoringOutputs, _ = strconv.Atoi(value)
			}
		case INPUT_LABELS:
			num, label, _ := strings.Cut(line, " ")
			number, err := strconv.Atoi(num)
			if err != nil {
				break
			}
			videohub.InputLabels[number] = label
		case OUTPUT_LABELS:
			num, label, _ := strings.Cut(line, " ")
			number, err := strconv.Atoi(num)
			if err != nil {
				break
			}
			videohub.OutputLabels[number] = label
		case ROUTING:
			out, in, _ := strings.Cut(line, " ")
			output, _ := strconv.Atoi(out)
			input, _ := strconv.Atoi(in)
			videohub.Routing[output] = input
		case LOCKS:
			out, lock, _ := strings.Cut(line, " ")
			output, _ := strconv.Atoi(out)
			videohub.Locks[output] = lock
		}
	}

	logger.Debug("Timeout, done parsing videohub response", "target", ip)
	return videohub, nil
}

func NewMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		scapeSuccess: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "scrape_success",
				Help:      "Displays whether or not the scrape was a success",
			},
		),
		scrapeDuration: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "scrape_duration_seconds",
				Help:      "Returns how long the scrape took to complete in seconds",
			},
		),
		info: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "device_info",
				Help:      "General device information",
			},
			[]string{"model_name", "name", "version", "inputs", "outputs", "processing_units", "monitoring_ouputs"},
		),
		numInputs: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "inputs_total",
				Help:      "The number of SDI inputs",
			},
		),
		numOutputs: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "outputs_total",
				Help:      "The number of SDI outputs",
			},
		),
		inputLabels: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "input_labels",
				Help:      "The label associated with each SDI input",
			},
			[]string{"input", "input_label"},
		),
		outputLabels: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "output_labels",
				Help:      "The label associated with each SDI output",
			},
			[]string{"output", "output_label"},
		),
		routing: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: *namespace,
				Name:      "routing",
				Help:      "The input routed to each output",
			},
			[]string{"output", "output_label", "input", "input_label"},
		),
	}

	reg.MustRegister(m.scapeSuccess)
	reg.MustRegister(m.scrapeDuration)
	reg.MustRegister(m.info)
	reg.MustRegister(m.numInputs)
	reg.MustRegister(m.numOutputs)
	reg.MustRegister(m.inputLabels)
	reg.MustRegister(m.outputLabels)
	reg.MustRegister(m.routing)
	return m
}

func SetVideoHubMetrics(videohub VideoHub, metrics *metrics) {

	metrics.info.With(prometheus.Labels{
		"model_name":        videohub.ModelName,
		"name":              videohub.FriendlyName,
		"version":           videohub.Version,
		"inputs":            strconv.Itoa(videohub.NumInputs),
		"outputs":           strconv.Itoa(videohub.NumOutputs),
		"processing_units":  strconv.Itoa(videohub.NumProcessingUnits),
		"monitoring_ouputs": strconv.Itoa(videohub.NumMonitoringOutputs),
	}).Set(1)

	metrics.numInputs.Set(float64(videohub.NumInputs))
	metrics.numOutputs.Set(float64(videohub.NumOutputs))

	for input, label := range videohub.InputLabels {
		metrics.inputLabels.With(prometheus.Labels{
			"input":       strconv.Itoa(input),
			"input_label": label,
		}).Set(float64(input))
	}

	for output, label := range videohub.OutputLabels {
		metrics.outputLabels.With(prometheus.Labels{
			"output":       strconv.Itoa(output),
			"output_label": label,
		}).Set(float64(output))
	}

	for output, input := range videohub.Routing {
		metrics.routing.With(prometheus.Labels{
			"output":       strconv.Itoa(output),
			"output_label": videohub.OutputLabels[output],
			"input":        strconv.Itoa(input),
			"input_label":  videohub.InputLabels[input],
		}).Set(float64(input))
	}
}
