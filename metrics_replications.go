package main

import (
	"encoding/json"
	"github.com/go-kit/kit/log"
	"strconv"
	"sync"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

type ReplicationsCollector struct {
	client    *HarborClient
	logger    log.Logger
	upChannel chan<- bool
	threads   int

	replicationUp     *prometheus.Desc
	replicationStatus *prometheus.Desc
	replicationTasks  *prometheus.Desc
}

func NewReplicationsCollector(c *HarborClient, l log.Logger, u chan<- bool, instance string, threads int) *ReplicationsCollector {
	return &ReplicationsCollector{
		client:    c,
		logger:    l,
		upChannel: u,
		threads:   threads,
		replicationUp: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, instance, "replication_up"),
			"Was the last query of harbor replications successful.",
			nil, nil,
		),
		replicationStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, instance, "replication_status"),
			"Get status of the last execution of this replication policy: Succeed = 1, any other status = 0.",
			[]string{"repl_pol_name"}, nil,
		),
		replicationTasks: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, instance, "replication_tasks"),
			"Get number of replication tasks, with various results, in the latest execution of this replication policy.",
			[]string{"repl_pol_name", "result"}, nil,
		),
	}
}

func (rc *ReplicationsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- rc.replicationUp
	ch <- rc.replicationStatus
	ch <- rc.replicationTasks
}

func (rc *ReplicationsCollector) Collect(ch chan<- prometheus.Metric) {
	type policiesMetric struct {
		Id   float64
		Name string
		// Extra fields omitted for maintainability: not relevant for current metrics
	}
	type policiesMetrics []policiesMetric
	type policyMetric []struct {
		Status      string
		Failed      float64
		Succeed     float64
		In_progress float64
		Stopped     float64
		// Extra fields omitted for maintainability: not relevant for current metrics
	}

	policiesBody := rc.client.request("/replication/policies")
	var policiesData policiesMetrics

	if err := json.Unmarshal(policiesBody, &policiesData); err != nil {
		level.Error(rc.logger).Log("msg", "Error retrieving replication policies", "err", err.Error())
		ch <- prometheus.MustNewConstMetric(
			rc.replicationUp, prometheus.GaugeValue, 0.0,
		)
		rc.upChannel <- false
		return
	}

	policyChan := make(chan policiesMetric, len(policiesData))
	defer close(policyChan)
	for _, p := range policiesData {
		policyChan <- p
	}
	threadgroup := sync.WaitGroup{}
	threadgroup.Add(rc.threads)
	policygroup := sync.WaitGroup{}
	policygroup.Add(len(policiesData))
	for i := 0; i < rc.threads; i++ {
		go func() {
			for {
				exit := false
				select {
				case policy := <-policyChan:

					policyId := strconv.FormatFloat(policy.Id, 'f', 0, 32)
					policyName := policy.Name

					body := rc.client.request("/replication/executions?policy_id=" + policyId + "&page=1&page_size=1")
					var data policyMetric

					if err := json.Unmarshal(body, &data); err != nil {
						level.Error(rc.logger).Log("msg", "Error retrieving replication data for policy "+policyId, "err", err.Error())
						ch <- prometheus.MustNewConstMetric(
							rc.replicationUp, prometheus.GaugeValue, 0.0,
						)
						rc.upChannel <- false
						return
					}

					for i := range data {
						var replStatus float64
						replStatus = 0
						if data[i].Status == "Succeed" {
							replStatus = 1
						}
						ch <- prometheus.MustNewConstMetric(
							rc.replicationStatus, prometheus.GaugeValue, replStatus, policyName,
						)
						ch <- prometheus.MustNewConstMetric(
							rc.replicationTasks, prometheus.GaugeValue, data[i].Failed, policyName, "failed",
						)
						ch <- prometheus.MustNewConstMetric(
							rc.replicationTasks, prometheus.GaugeValue, data[i].Succeed, policyName, "succeed",
						)
						ch <- prometheus.MustNewConstMetric(
							rc.replicationTasks, prometheus.GaugeValue, data[i].In_progress, policyName, "in_progress",
						)
						ch <- prometheus.MustNewConstMetric(
							rc.replicationTasks, prometheus.GaugeValue, data[i].Stopped, policyName, "stopped",
						)
					}
					policygroup.Done()
				default:
					exit = true
					break
				}
				if exit {
					break
				}
			}
			threadgroup.Done()
		}()
	}

	policygroup.Wait()
	threadgroup.Wait()
	ch <- prometheus.MustNewConstMetric(
		rc.replicationUp, prometheus.GaugeValue, 1.0,
	)
	rc.upChannel <- true
}
