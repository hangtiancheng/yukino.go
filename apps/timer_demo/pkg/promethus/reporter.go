package promethus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type monitorComponentType string

const (
	counter monitorComponentType = "counter"
	summary monitorComponentType = "summary"
	gauge   monitorComponentType = "gauge"

	// Total count of timer executions.
	timerExecTotalCnt        = "timer_exec_total_cnt"
	timerExecTotalCntSummary = "Total count of timer executions"

	// Timer execution delay.
	timerDelayCnt        = "timer_delay_cnt"
	timerDelayCntSummary = "Timer execution delay"

	// Total number of enabled timers.
	timerEnabledCnt        = "timer_enabled_cnt"
	timerEnabledCntSummary = "Total number of enabled timers"

	// Number of unexecuted timers.
	timerNoExceedCnt        = "timer_no_exceed_cnt"
	timerNoExceedCntSummary = "Number of unexecuted timers"

	reportName = "_name"
	reportType = "_type"
	timerApp   = "timer_demoApp"

	label = "label"
	timer = "timer"
)

// Reporter is the monitoring metrics reporter.
type Reporter struct {
	timerExecRecorder     *prometheus.CounterVec
	timeDelayRecorder     prometheus.ObserverVec
	timerEnabledRecorder  *prometheus.GaugeVec
	timerNoExceedRecorder *prometheus.GaugeVec
}

var reporter = newReporter()

// GetReporter returns the singleton reporter instance.
func GetReporter() *Reporter {
	return reporter
}

func newReporter() *Reporter {
	r := Reporter{
		timerExecRecorder: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: timerExecTotalCnt,
			Help: timerExecTotalCntSummary,
		}, []string{
			timerApp,
			reportName,
			reportType,
		}).MustCurryWith(prometheus.Labels{reportName: timerExecTotalCntSummary,
			reportType: string(counter)}),

		timeDelayRecorder: promauto.NewSummaryVec(prometheus.SummaryOpts{
			Name:       timerDelayCnt,
			Help:       timerDelayCntSummary,
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001, 0.999: 0.0001, 0.9999: 0.00001},
		}, []string{
			timerApp,
			reportName,
			reportType,
		}).MustCurryWith(prometheus.Labels{reportName: timerDelayCntSummary,
			reportType: string(summary)}),

		timerEnabledRecorder: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: timerEnabledCnt,
			Help: timerEnabledCntSummary,
		}, []string{
			label,
			reportName,
			reportType,
		}).MustCurryWith(prometheus.Labels{reportName: timerEnabledCntSummary,
			reportType: string(gauge)}),

		timerNoExceedRecorder: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: timerNoExceedCnt,
			Help: timerNoExceedCntSummary,
		}, []string{
			label,
			reportName,
			reportType,
		}).MustCurryWith(prometheus.Labels{reportName: timerNoExceedCntSummary,
			reportType: string(gauge)}),
	}

	return &r
}

func (r *Reporter) ReportExecRecord(app string) {
	r.timerExecRecorder.WithLabelValues(app).Inc()
}

func (r *Reporter) ReportTimerDelayRecord(app string, cost float64) {
	r.timeDelayRecorder.WithLabelValues(app).Observe(cost)
}

func (r *Reporter) ReportTimerEnabledRecord(total float64) {
	r.timerEnabledRecorder.WithLabelValues(timer).Set(total)
}

func (r *Reporter) ReportTimerNoExceedRecord(total float64) {
	r.timerNoExceedRecorder.WithLabelValues(timer).Set(total)
}
