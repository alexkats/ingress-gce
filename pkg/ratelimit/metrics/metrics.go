/*
Copyright 2023 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"sync"
	"time"

	"k8s.io/ingress-gce/pkg/utils"

	"github.com/prometheus/client_golang/prometheus"
)

var register sync.Once

const (
	rateLimitSubsystem       = "rate_limit"
	rateLimitLatencyKey      = "rate_limit_delay_seconds"
	strategyLatencyKey       = "strategy_delay_seconds"
	strategyPossibleDelayKey = "strategy_possible_delay_value_seconds"
	strategyLockLatencyKey   = "strategy_lock_delay_seconds"
	quotaExceededCounterKey  = "quota_exceeded_counter"
	otherErrorsCounterKey    = "other_errors_counter"
	noErrorsCounterKey       = "no_errors_counter"
)

var (
	rateLimitLatencyMetricsLabels = []string{
		"key", // rate limiter key
	}

	strategyLatencyMetricsLabels = []string{
		"key", // rate limiter key
	}

	strategyPossibleDelayMetricsLabels = []string{
		"key", // rate limiter key
	}

	strategyLockLatencyMetricsLabels = []string{
		"key", // rate limiter key
	}

	quotaExceededCounterMetricsLabels = []string{
		"key", // rate limiter key
	}

	otherErrorsCounterMetricsLabels = []string{
		"key", // rate limiter key
	}

	noErrorsCounterMetricsLabels = []string{
		"key", // rate limiter key
	}

	RateLimitLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimitSubsystem,
			Name:      rateLimitLatencyKey,
			Help:      "Latency of the RateLimiter Accept Operation",
			// custom buckets = [0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048s(~34min), 4096s(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 14),
		},
		rateLimitLatencyMetricsLabels,
	)

	StrategyLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimitSubsystem,
			Name:      strategyLatencyKey,
			Help:      "Latency of the StrategyRateLimiter Accept Operation",
			// custom buckets = [0.5s, 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s(~4min), 512s(~8min), 1024s(~17min), 2048s(~34min), 4096s(~68min), +Inf]
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 14),
		},
		strategyLatencyMetricsLabels,
	)

	StrategyPossibleDelay = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimitSubsystem,
			Name:      strategyPossibleDelayKey,
			Help:      "Delay values most likely used for StrategyRateLimiter",
			// custom buckets = [0.1s, 0.2s, 0.4s, 0.8s, 1.6s, 3.2s, 6.4s, 12.8s, 25.6s, 51.2s, 102.4s, 204.8s(~3min), 409.6s(~7min), 819.2s(~14min), +Inf]
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 14),
		},
		strategyPossibleDelayMetricsLabels,
	)

	StrategyLockLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: rateLimitSubsystem,
			Name:      strategyLockLatencyKey,
			Help:      "Latency of the lock acquisition for the StrategyRateLimiter",
			Buckets:   append(prometheus.ExponentialBuckets(1e-9, 10, 8), prometheus.ExponentialBuckets(0.1, 2, 14)...),
		},
		strategyLockLatencyMetricsLabels,
	)

	QuotaExceededCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: rateLimitSubsystem,
			Name:      quotaExceededCounterKey,
			Help:      "Number of quota exceeded errors for the rate limiter",
		},
		quotaExceededCounterMetricsLabels,
	)

	OtherErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: rateLimitSubsystem,
			Name:      otherErrorsCounterKey,
			Help:      "Number of other errors for the rate limiter",
		},
		otherErrorsCounterMetricsLabels,
	)

	NoErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: rateLimitSubsystem,
			Name:      noErrorsCounterKey,
			Help:      "Number of successful requests for the rate limiter",
		},
		noErrorsCounterMetricsLabels,
	)
)

func RegisterMetrics() {
	register.Do(func() {
		prometheus.MustRegister(RateLimitLatency)
		prometheus.MustRegister(StrategyLatency)
		prometheus.MustRegister(StrategyPossibleDelay)
		prometheus.MustRegister(StrategyLockLatency)
		prometheus.MustRegister(QuotaExceededCounter)
		prometheus.MustRegister(OtherErrorsCounter)
		prometheus.MustRegister(NoErrorsCounter)
	})
}

func PublishRateLimiterMetrics(key string, start time.Time) {
	RateLimitLatency.WithLabelValues(key).Observe(time.Since(start).Seconds())
}

func PublishStrategyRateLimiterMetrics(key string, start time.Time, possibleDelay time.Duration, lockLatency time.Duration) {
	StrategyLatency.WithLabelValues(key).Observe(time.Since(start).Seconds())
	StrategyPossibleDelay.WithLabelValues(key).Observe(possibleDelay.Seconds())
	StrategyLockLatency.WithLabelValues(key).Observe(lockLatency.Seconds())
}

func PublishQuotaExceededRateLimiterMetrics(key string, err error) {
	if utils.IsQuotaExceededError(err) {
		QuotaExceededCounter.WithLabelValues(key).Inc()
	} else if err != nil {
		OtherErrorsCounter.WithLabelValues(key).Inc()
	} else {
		NoErrorsCounter.WithLabelValues(key).Inc()
	}
}
