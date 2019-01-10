package container

import (
	"context"
	"encoding/json"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"strings"
	"sync"
)

type safeDescMap struct {
	sMap map[string]float64
	lock *sync.Mutex
}

type ContainerDesc struct {
	CPUUsagePercentageDesc    *prometheus.Desc
	MemoryUsagePercentageDesc *prometheus.Desc
	MemoryUsageDesc           *prometheus.Desc
	MemoryLimitDesc           *prometheus.Desc
}

func NewContainerDesc() *ContainerDesc {
	return &ContainerDesc{
		CPUUsagePercentageDesc:    prometheus.NewDesc("cpu_usage_percentage", "docker stats", []string{"pod_name", "container_name"}, prometheus.Labels{"type": "caas"}),
		MemoryUsagePercentageDesc: prometheus.NewDesc("memory_usage_percentage", "docker stats", []string{"pod_name", "container_name"}, prometheus.Labels{"type": "caas"}),
		MemoryUsageDesc:           prometheus.NewDesc("memory_usage", "docker stats", []string{"pod_name", "container_name"}, prometheus.Labels{"type": "caas"}),
		MemoryLimitDesc:           prometheus.NewDesc("memory_limit", "docker stats", []string{"pod_name", "container_name"}, prometheus.Labels{"type": "caas"}),
	}
}

func (c *ContainerDesc) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.CPUUsagePercentageDesc
	ch <- c.MemoryUsagePercentageDesc
	ch <- c.MemoryUsageDesc
	ch <- c.MemoryLimitDesc
}

func (c *ContainerDesc) Collect(ch chan<- prometheus.Metric) {
	cpuUsagePercentageMap, memoryUsagePercentageMap, memoryUsageMap, memoryLimitMap := c.GenItemsMap()
	for info, value := range cpuUsagePercentageMap {
		infoArr := strings.Split(info, "%")
		if len(infoArr) > 1 {
			ch <- prometheus.MustNewConstMetric(c.CPUUsagePercentageDesc, prometheus.GaugeValue, value, infoArr[0], infoArr[1])
		}
	}

	for info, value := range memoryUsagePercentageMap {
		infoArr := strings.Split(info, "%")
		if len(infoArr) > 1 {
			ch <- prometheus.MustNewConstMetric(c.MemoryUsagePercentageDesc, prometheus.GaugeValue, value, infoArr[0], infoArr[1])
		}
	}

	for info, value := range memoryUsageMap {
		infoArr := strings.Split(info, "%")
		if len(infoArr) > 1 {
			ch <- prometheus.MustNewConstMetric(c.MemoryUsageDesc, prometheus.GaugeValue, value, infoArr[0], infoArr[1])
		}
	}

	for info, value := range memoryLimitMap {
		infoArr := strings.Split(info, "%")
		if len(infoArr) > 1 {
			ch <- prometheus.MustNewConstMetric(c.MemoryLimitDesc, prometheus.GaugeValue, value, infoArr[0], infoArr[1])
		}
	}
}

func (c *ContainerDesc) GenItemsMap() (
	cpuUsagePercentageMap map[string]float64, memoryUsagePercentageMap map[string]float64, memoryUsageMap map[string]float64, memoryLimitMap map[string]float64) {
	cpuUsagePercentageMap = make(map[string]float64)
	memoryUsagePercentageMap = make(map[string]float64)
	memoryUsageMap = make(map[string]float64)
	memoryLimitMap = make(map[string]float64)

	wg := &sync.WaitGroup{}

	cpuUsagePercentageSafeMap := &safeDescMap{
		sMap: cpuUsagePercentageMap,
		lock: &sync.Mutex{},
	}

	memoryUsagePercentageSafeMap := &safeDescMap{
		sMap: memoryUsagePercentageMap,
		lock: &sync.Mutex{},
	}

	memoryUsageSafeMap := &safeDescMap{
		sMap: memoryUsageMap,
		lock: &sync.Mutex{},
	}

	memoryLimitSafeMap := &safeDescMap{
		sMap: memoryLimitMap,
		lock: &sync.Mutex{},
	}

	dockerCli, err := client.NewEnvClient()
	if err != nil {
		logrus.Infoln(err.Error())
	}

	defer func () {
		if err := dockerCli.Close();err != nil {
			logrus.Infoln("docker api close error ", err.Error())
		}
	}()

	containers, err := dockerCli.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		logrus.Infoln(err.Error())
		return
	}

	kubeCons := make([]types.Container, 0)

	for _, con := range containers {
		cType, hasType := con.Labels["io.kubernetes.docker.type"]
		_, hasPodName := con.Labels["io.kubernetes.pod.name"]
		_, hasContainerName := con.Labels["io.kubernetes.container.name"]
		if hasType && hasPodName && hasContainerName && cType == "container" {
			kubeCons = append(kubeCons, con)
		}
	}

	if len(kubeCons) > 0 {

		wg.Add(len(kubeCons))

		for _, c := range kubeCons {

			pName, _ := c.Labels["io.kubernetes.pod.name"]
			cName, _ := c.Labels["io.kubernetes.container.name"]

			go func(podName string, containerName string, containerId string) {

				stats, err := dockerCli.ContainerStats(context.Background(), containerId, false)
				if err != nil {
					logrus.Infoln(err.Error())
					wg.Done()
					return
				}

				var v *types.StatsJSON

				err = json.NewDecoder(stats.Body).Decode(&v)
				if err != nil {
					logrus.Infoln(err.Error())
					wg.Done()
					return
				}

				cpuDelta := float64(v.CPUStats.CPUUsage.TotalUsage - v.PreCPUStats.CPUUsage.TotalUsage)
				systemDelta := float64(v.CPUStats.SystemUsage - v.PreCPUStats.SystemUsage)
				cpuPercent := (cpuDelta / systemDelta) * float64(len(v.CPUStats.CPUUsage.PercpuUsage)) * 100.0

				cpuUsagePercentageSafeMap.lock.Lock()
				cpuUsagePercentageSafeMap.sMap[podName+"%"+containerName] = cpuPercent
				cpuUsagePercentageSafeMap.lock.Unlock()

				memoryUsagePercentageSafeMap.lock.Lock()
				memoryUsagePercentageSafeMap.sMap[podName+"%"+containerName] = float64(v.MemoryStats.Usage * 100) / float64(v.MemoryStats.Limit)
				memoryUsagePercentageSafeMap.lock.Unlock()

				memoryUsageSafeMap.lock.Lock()
				memoryUsageSafeMap.sMap[podName+"%"+containerName] = float64(v.MemoryStats.Usage)
				memoryUsageSafeMap.lock.Unlock()

				memoryLimitSafeMap.lock.Lock()
				memoryLimitSafeMap.sMap[podName+"%"+containerName] = float64(v.MemoryStats.Limit)
				memoryLimitSafeMap.lock.Unlock()
				stats.Body.Close()
				wg.Done()
			}(pName, cName, c.ID)
		}
	}
	wg.Wait()
	return
}
