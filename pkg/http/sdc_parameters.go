package http

import (
	"context"

	"github.com/degoke/health-ai-stack/pkg/sdc"
)

func operationParameters(req SDCRequest) sdc.OperationParameters {
	if req.Parameters != nil {
		return sdc.ParseOperationParameters(req.Parameters)
	}
	if req.Body != nil && req.Body.ResourceType == "Parameters" {
		return sdc.ParseOperationParameters(req.Body)
	}
	return sdc.OperationParameters{}
}

func validationOptions(a CoreSDCService, ctx context.Context, req SDCRequest) sdc.ValidationOptions {
	opts := sdc.ValidationOptions{
		Ctx:         ctx,
		Expressions: a.Provider,
		Terminology: a.Terminology,
	}
	if a.Resources != nil {
		opts.References = sdc.ResourceServiceReferenceResolver{Service: a.Resources}
	}
	return sdc.ValidationOptionsFromParameters(operationParameters(req), opts)
}

func populationContext(a CoreSDCService, req SDCRequest, initial *sdc.QuestionnaireResponse) sdc.PopulationContext {
	pc := sdc.PopulationContext{InitialResponse: initial, Provider: a.Provider}
	return sdc.PopulationContextFromParameters(operationParameters(req), pc)
}
