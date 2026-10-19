package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
)

func TestSetAppBuildPairedWithReplacesSeries(t *testing.T) {
	reg := initTestMetricsRegistry(t)
	t.Cleanup(func() {
		appBuildInfo = nil
		appBuildBaseLabels = nil
		appBuildPairedWith = ""
	})

	// A mint before metrics exist must not create a series.
	SetAppBuildPairedWith("relay")
	InitMetricsWithRegistry(logger.NewAppSLogger(logger.Debug), "partner")
	require.Equal(t, "partner", onlyPairedWith(t, reg))

	SetAppBuildPairedWith("partner")
	require.Equal(t, "partner", onlyPairedWith(t, reg))

	SetAppBuildPairedWith("relay")
	require.Equal(t, "relay", onlyPairedWith(t, reg))
}

func onlyPairedWith(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	family := metricFamilyByName(t, reg, testMetricsNamespace+"_"+testMetricsSubsystem+"_app_build_info")
	require.Len(t, family.Metric, 1)
	require.Equal(t, 1.0, family.Metric[0].GetGauge().GetValue())
	for _, label := range family.Metric[0].Label {
		if label.GetName() == "paired_with" {
			return label.GetValue()
		}
	}
	t.Fatal("paired_with label missing")
	return ""
}
