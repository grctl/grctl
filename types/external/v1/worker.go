package external

type WorkerResponseStatus string

const WorkerResponseStatusAccepted WorkerResponseStatus = "accepted"
const WorkerResponseStatusRejected WorkerResponseStatus = "rejected"

type WorkerError struct {
	// Error code
	Code string `json:"code" msgpack:"code"`

	// Error message
	Message string `json:"message" msgpack:"message"`

	// Whether error is retryable
	Retryable bool `json:"retryable" msgpack:"retryable"`
}

type WorkerResponse struct {
	// Error corresponds to the JSON schema field "error".
	Error *WorkerError `json:"error,omitempty" msgpack:"error,omitempty"`

	// Optional response message
	Message *string `json:"message,omitempty" msgpack:"message,omitempty"`

	// Status corresponds to the JSON schema field "status".
	Status WorkerResponseStatus `json:"status" msgpack:"status"`
}
