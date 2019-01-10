package main

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"github.com/anzersy/MCE/v1/container"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	_ "net/http/pprof"
)

func main () {
	containerDesc := container.NewContainerDesc()
	reg := prometheus.NewRegistry()
	reg.MustRegister(containerDesc)

	ga :=prometheus.Gatherers{reg}
	h := promhttp.HandlerFor(ga,promhttp.HandlerOpts{
		ErrorLog: logrus.New(),
		ErrorHandling:promhttp.ContinueOnError,
	})
	http.Handle("/metrics",h)
	logrus.Infoln("start mce")
	go func(){
		http.ListenAndServe(":8111", nil)
	}()
	logrus.Fatal(http.ListenAndServe("0.0.0.0:8555",nil))
}