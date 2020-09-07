package injector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	admission "k8s.io/api/admission/v1"
)

// ObjectMutator is capable of mutating an object.
type ObjectMutator interface {
	Mutate(req *admission.AdmissionRequest) (*admission.AdmissionResponse, error)
}

// API is an HTTP server which handle admission webhooks.
type API struct {
	http.Server

	mutator ObjectMutator
	log     logrus.FieldLogger
}

// NewAPI creates and configure a new API.
func NewAPI(log logrus.FieldLogger, addr string, cert tls.Certificate, mutator ObjectMutator) *API {
	router := mux.NewRouter()

	api := &API{
		Server: http.Server{
			Addr:    addr,
			Handler: router,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		},
		mutator: mutator,
		log:     log,
	}

	router.HandleFunc("/health", api.health)
	router.HandleFunc("/inject", api.inject)

	return api
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (a *API) inject(w http.ResponseWriter, r *http.Request) {
	var (
		review admission.AdmissionReview
		err    error
	)

	if err = json.NewDecoder(r.Body).Decode(&review); err != nil {
		a.log.Errorf("Unable to deserialize admission review: %v", err)
		http.Error(w, "", http.StatusBadRequest)
		return
	}

	review.Response, err = a.mutator.Mutate(review.Request)
	if err != nil {
		a.log.Errorf("Unable to mutate %q %q in namespace %q: %v",
			review.Request.Kind.Kind,
			review.Request.Name,
			review.Request.Namespace,
			err)

		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if err = json.NewEncoder(w).Encode(review); err != nil {
		a.log.Errorf("Unable to serialize admission review: %v", err)
		http.Error(w, "", http.StatusInternalServerError)
	}
}

// Start starts the API.
func (a *API) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		if err := a.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("API has stopped unexpectedly: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := a.Shutdown(stopCtx); err != nil {
			return fmt.Errorf("unable to stop the API server: %w", err)
		}
	case err := <-errCh:
		return err
	}

	return nil
}
