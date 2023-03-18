/*
Copyright 2018 The Kubernetes Authors.

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

package syncers

import (
	"fmt"
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/neg/backoff"
	negtypes "k8s.io/ingress-gce/pkg/neg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
)

type syncerCore interface {
	sync() error
}

// syncer is a NEG syncer skeleton.
// It handles state transitions and backoff retry operations.
type syncer struct {
	// metadata
	negtypes.NegSyncerKey

	// NEG sync function
	core syncerCore

	// event recording
	serviceLister cache.Indexer
	recorder      record.EventRecorder

	// syncer states
	stateLock    sync.Mutex
	stopped      bool
	shuttingDown bool

	// sync signal and retry handling
	syncCh  chan interface{}
	clock   clock.Clock
	backoff backoff.BackoffHandler

	logger klog.Logger
}

func newSyncer(negSyncerKey negtypes.NegSyncerKey, serviceLister cache.Indexer, recorder record.EventRecorder, core syncerCore, logger klog.Logger) *syncer {
	return &syncer{
		NegSyncerKey:  negSyncerKey,
		core:          core,
		serviceLister: serviceLister,
		recorder:      recorder,
		stopped:       true,
		shuttingDown:  false,
		clock:         clock.RealClock{},
		backoff:       backoff.NewExponentialBackoffHandler(maxRetries, minRetryDelay, maxRetryDelay),
		logger:        logger,
	}
}

func (s *syncer) Start() error {
	if !s.IsStopped() {
		return fmt.Errorf("NEG syncer for %s is already running.", s.NegSyncerKey.String())
	}
	if s.IsShuttingDown() {
		return fmt.Errorf("NEG syncer for %s is shutting down. ", s.NegSyncerKey.String())
	}

	s.logger.V(2).Info("alexkats: main: Syncer: Starting NEG syncer for service port", "negSyncerKey", s.NegSyncerKey.String())
	s.init()
	go func() {
		for {
			s.logger.V(3).Info("alexkats: main: Syncer: Going to sync", "negSyncerKey", s.NegSyncerKey.String())
			// equivalent to never retry
			retryCh := make(<-chan time.Time)
			err := s.core.sync()
			s.logger.V(3).Info("alexkats: main: Syncer: Sync invocation finished", "negSyncerKey", s.NegSyncerKey.String(), "err", err)
			if err != nil {
				delay, retryErr := time.Duration(0), error(nil)
				if syncErr := negtypes.ClassifyError(err); syncErr.Reason != negtypes.ReasonQuotaExceededWithStrategy {
					s.logger.Error(err, "alexkats: main: Syncer: NOT Quota exceeded error")
					delay, retryErr = s.backoff.NextDelay()
				}
				s.logger.V(3).Info("alexkats: main: Syncer: Start 1", "negSyncerKey", s.NegSyncerKey.String())
				retryMsg := ""
				if retryErr == backoff.ErrRetriesExceeded {
					retryMsg = "(will not retry)"
					s.logger.V(3).Info("alexkats: main: Syncer: Start 2.1", "negSyncerKey", s.NegSyncerKey.String())
				} else {
					retryCh = s.clock.After(delay)
					retryMsg = "(will retry)"
					s.logger.V(3).Info("alexkats: main: Syncer: Start 2.2", "negSyncerKey", s.NegSyncerKey.String())
				}

				s.logger.V(3).Info("alexkats: main: Syncer: Start 3", "negSyncerKey", s.NegSyncerKey.String())
				if svc := getService(s.serviceLister, s.Namespace, s.Name); svc != nil {
					s.logger.V(3).Info(fmt.Sprintf("alexkats: main: Syncer: Failed to sync NEG %q with delay %q %s: %v", s.NegSyncerKey.NegName, delay, retryMsg, err))
					s.recorder.Eventf(svc, apiv1.EventTypeWarning, "SyncNetworkEndpointGroupFailed", "Failed to sync NEG %q %s: %v", s.NegSyncerKey.NegName, retryMsg, err)
				}
				s.logger.V(3).Info("alexkats: main: Syncer: Start 4", "negSyncerKey", s.NegSyncerKey.String())
			} else {
				s.logger.V(3).Info("alexkats: main: Syncer: Start 1.2", "negSyncerKey", s.NegSyncerKey.String())
				s.backoff.ResetDelay()
			}

			select {
			case _, open := <-s.syncCh:
				s.logger.V(3).Info("alexkats: main: Syncer: Got syncCh", "negSyncerKey", s.NegSyncerKey.String())
				if !open {
					s.logger.V(3).Info("alexkats: main: Syncer: Going to stop NEG syncer", "negSyncerKey", s.NegSyncerKey.String())
					s.stateLock.Lock()
					s.shuttingDown = false
					s.stateLock.Unlock()
					s.logger.V(2).Info("alexkats: main: Syncer: Stopped NEG syncer", "negSyncerKey", s.NegSyncerKey.String())
					return
				}
			case <-retryCh:
				s.logger.V(3).Info("alexkats: main: Syncer: Triggerred retryCh", "negSyncerKey", s.NegSyncerKey.String())
				// continue to sync
			}
		}
	}()
	return nil
}

func (s *syncer) init() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	s.stopped = false
	s.syncCh = make(chan interface{}, 1)
}

func (s *syncer) Stop() {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	if !s.stopped {
		s.logger.V(2).Info("alexkats: main: Syncer: Stopping NEG syncer for service port", "negSyncerKey", s.NegSyncerKey.String())
		s.stopped = true
		s.shuttingDown = true
		close(s.syncCh)
	}
}

func (s *syncer) Sync() bool {
	if s.IsStopped() {
		s.logger.Info("alexkats: main: Syncer: NEG syncer is already stopped.", "negSyncerKey", s.NegSyncerKey.String())
		return false
	}
	select {
	case s.syncCh <- struct{}{}:
		s.logger.Info("alexkats: main: Syncer: Sent to syncCh", "negSyncerKey", s.NegSyncerKey.String())
		return true
	default:
		s.logger.Info("alexkats: main: Syncer: DID NOT Send to syncCh", "negSyncerKey", s.NegSyncerKey.String())
		return false
	}
}

func (s *syncer) IsStopped() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	return s.stopped
}

func (s *syncer) IsShuttingDown() bool {
	s.stateLock.Lock()
	defer s.stateLock.Unlock()
	return s.shuttingDown
}
