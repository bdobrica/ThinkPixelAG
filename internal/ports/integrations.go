package ports

import "context"

type OPAConnection struct {
	Endpoint       string `json:"endpoint"`
	TokenReference string `json:"token_reference"`
}
type IntegrationSettings struct {
	ID         string        `json:"id"`
	Mode       string        `json:"mode"`
	Revision   int64         `json:"revision"`
	Connection OPAConnection `json:"connection"`
}
type IntegrationStore interface {
	OPAIntegration(context.Context) (IntegrationSettings, error)
	SaveOPAIntegration(context.Context, AdministrationOperation, int64, OPAConnection) (IntegrationSettings, error)
}
type IntegrationChecker interface {
	Check(context.Context, OPAConnection) error
}
