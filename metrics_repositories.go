package main

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
)

func (e *HarborExporter) collectRepositoriesMetric(ch chan<- prometheus.Metric) bool {
	type projectsMetric struct {
		Project_id  float64
		Owner_id    float64
		Name        string
		Repo_count  float64
		Chart_count float64
	}
	type repositoriesMetric []struct {
		Id            float64
		Name          string
		Project_id    float64
		Description   string
		Pull_count    float64
		Star_count    float64
		Tags_count    float64
		Creation_time time.Time
		Update_time   time.Time
		labels        []struct {
			Id            float64
			Name          string
			Project_id    float64
			Description   string
			Color         string
			Deleted       bool
			Scope         string
			Creation_time time.Time
			Update_time   time.Time
		}
	}
	type repositoriesMetricV2 []struct {
		Id             float64
		Name           string
		Project_id     float64
		Description    string
		Pull_count     float64
		Artifact_count float64
		Creation_time  time.Time
		Update_time    time.Time
	}
	var projectsData []interface{}
	err := e.requestAll("/projects", func(pageBody []byte) error {
		var pageData []projectsMetric
		if err := json.Unmarshal(pageBody, &pageData); err != nil {
			return err
		}
		for _, i := range pageData {
			projectsData = append(projectsData, i)
		}

		return nil
	})
	if err != nil {
		level.Error(e.logger).Log(err.Error())
		return false
	}
	repoFunc := func(data interface{}) error {
		project := data.(projectsMetric)
		projectId := strconv.FormatFloat(project.Project_id, 'f', 0, 32)
		if e.isV2 {
			var data repositoriesMetricV2
			err := e.requestAll("/projects/"+project.Name+"/repositories", func(pageBody []byte) error {
				var pageData repositoriesMetricV2
				if err := json.Unmarshal(pageBody, &pageData); err != nil {
					return err
				}

				data = append(data, pageData...)

				return nil
			})
			if err != nil {
				level.Error(e.logger).Log(err.Error())
				return err
			}

			for i := range data {
				repoId := strconv.FormatFloat(data[i].Id, 'f', 0, 32)
				ch <- prometheus.MustNewConstMetric(
					allMetrics["repositories_pull_total"].Desc, allMetrics["repositories_pull_total"].Type, data[i].Pull_count, data[i].Name, repoId,
				)
				// ch <- prometheus.MustNewConstMetric(
				// 	allMetrics["repositories_star_total"].Desc, allMetrics["repositories_star_total"].Type, data[i].Star_count, data[i].Name, repoId,
				// )
				ch <- prometheus.MustNewConstMetric(
					allMetrics["repositories_tags_total"].Desc, allMetrics["repositories_tags_total"].Type, data[i].Artifact_count, data[i].Name, repoId,
				)
			}

		} else {
			var data repositoriesMetric
			err := e.requestAll("/repositories?project_id="+projectId, func(pageBody []byte) error {
				var pageData repositoriesMetric
				if err := json.Unmarshal(pageBody, &pageData); err != nil {
					return err
				}

				data = append(data, pageData...)

				return nil
			})
			if err != nil {
				level.Error(e.logger).Log(err.Error())
				return err
			}

			for i := range data {
				repoId := strconv.FormatFloat(data[i].Id, 'f', 0, 32)
				ch <- prometheus.MustNewConstMetric(
					allMetrics["repositories_pull_total"].Desc, allMetrics["repositories_pull_total"].Type, data[i].Pull_count, data[i].Name, repoId,
				)
				ch <- prometheus.MustNewConstMetric(
					allMetrics["repositories_star_total"].Desc, allMetrics["repositories_star_total"].Type, data[i].Star_count, data[i].Name, repoId,
				)
				ch <- prometheus.MustNewConstMetric(
					allMetrics["repositories_tags_total"].Desc, allMetrics["repositories_tags_total"].Type, data[i].Tags_count, data[i].Name, repoId,
				)
			}
		}
		return nil
	}

	return e.doWork(repoFunc, projectsData) == nil
}
