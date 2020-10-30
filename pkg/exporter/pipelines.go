package exporter

import (
	"fmt"
	"reflect"
	"strconv"

	"github.com/mvisonneau/gitlab-ci-pipelines-exporter/pkg/schemas"
	log "github.com/sirupsen/logrus"
	goGitlab "github.com/xanzy/go-gitlab"
)

func pullRefMetrics(ref schemas.Ref) error {
	logFields := log.Fields{
		"project-name":     ref.PathWithNamespace,
		"project-id":       ref.ID,
		"project-ref":      ref.Ref,
		"project-ref-kind": ref.Kind,
	}

	// TODO: Figure out if we want to have a similar approach for RefKindTag with
	// an additional configuration parameeter perhaps
	if ref.Kind == schemas.RefKindMergeRequest && ref.MostRecentPipeline != nil {
		switch ref.MostRecentPipeline.Status {
		case "success", "failed", "canceled", "skipped":
			// The pipeline will not evolve, lets not bother querying the API
			log.WithFields(logFields).WithField("most-recent-pipeline-id", ref.MostRecentPipeline.ID).Debug("skipping finished merge-request pipeline")
			return nil
		}
	}

	cfgUpdateLock.RLock()
	defer cfgUpdateLock.RUnlock()
	pipelines, err := gitlabClient.GetProjectPipelines(ref.ID, &goGitlab.ListProjectPipelinesOptions{
		// We only need the most recent pipeline
		ListOptions: goGitlab.ListOptions{
			PerPage: 1,
			Page:    1,
		},
		Ref: goGitlab.String(ref.Ref),
	})

	if err != nil {
		return fmt.Errorf("error fetching project pipelines for %s: %v", ref.PathWithNamespace, err)
	}

	if len(pipelines) == 0 {
		log.WithFields(logFields).Debug("could not find any pipeline for the project ref")
		return nil
	}

	pipeline, err := gitlabClient.GetRefPipeline(ref, pipelines[0].ID)
	if err != nil {
		return err
	}

	if ref.MostRecentPipeline == nil || !reflect.DeepEqual(pipeline, ref.MostRecentPipeline) {
		ref.MostRecentPipeline = pipeline

		// fetch pipeline variables
		if ref.Pull.Pipeline.Variables.Enabled() {
			ref.MostRecentPipelineVariables, err = gitlabClient.GetRefPipelineVariablesAsConcatenatedString(ref)
			if err != nil {
				return err
			}
		}

		if pipeline.Status == "running" {
			runCount := schemas.Metric{
				Kind:   schemas.MetricKindRunCount,
				Labels: ref.DefaultLabelsValues(),
			}
			storeGetMetric(&runCount)
			runCount.Value++
			storeSetMetric(runCount)
		}

		if pipeline.Coverage != "" {
			parsedCoverage, err := strconv.ParseFloat(pipeline.Coverage, 64)
			if err != nil {
				log.WithFields(logFields).WithField("error", err.Error()).Warnf("could not parse coverage string returned from GitLab API '%s' into Float64", pipeline.Coverage)
			} else {
				storeSetMetric(schemas.Metric{
					Kind:   schemas.MetricKindCoverage,
					Labels: ref.DefaultLabelsValues(),
					Value:  parsedCoverage,
				})
			}
		}

		storeSetMetric(schemas.Metric{
			Kind:   schemas.MetricKindID,
			Labels: ref.DefaultLabelsValues(),
			Value:  float64(pipeline.ID),
		})

		emitStatusMetric(
			schemas.MetricKindStatus,
			ref.DefaultLabelsValues(),
			statusesList[:],
			pipeline.Status,
			ref.OutputSparseStatusMetrics(),
		)

		storeSetMetric(schemas.Metric{
			Kind:   schemas.MetricKindDurationSeconds,
			Labels: ref.DefaultLabelsValues(),
			Value:  float64(pipeline.Duration),
		})

		storeSetMetric(schemas.Metric{
			Kind:   schemas.MetricKindTimestamp,
			Labels: ref.DefaultLabelsValues(),
			Value:  float64(pipeline.UpdatedAt.Unix()),
		})

		if ref.Pull.Pipeline.Jobs.Enabled() {
			if err = pullRefPipelineJobsMetrics(ref); err != nil {
				return err
			}
		}
	}

	if ref.Pull.Pipeline.Jobs.Enabled() {
		if err := pullRefMostRecentJobsMetrics(ref); err != nil {
			return err
		}
	}

	return nil
}
