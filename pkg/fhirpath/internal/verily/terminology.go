package verily

import (
	"context"

	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
	pgp "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/resources/parameters_go_proto"
	"github.com/verily-src/fhirpath-go/fhirpath/terminology"
)

// TerminologyValidator validates code membership in a value set.
type TerminologyValidator interface {
	MemberOf(ctx context.Context, valueSetURL, system, code string) (bool, error)
}

type terminologyAdapter struct {
	validator TerminologyValidator
}

func (a *terminologyAdapter) ValueSetValidateCode(ctx context.Context, opts *terminology.ValueSetValidateCodeOptions) (*pgp.Parameters, error) {
	if a == nil || a.validator == nil || opts == nil {
		return parametersResult(false), nil
	}
	ok, err := a.validator.MemberOf(ctx, opts.ID, opts.System, opts.Code)
	if err != nil {
		return nil, err
	}
	return parametersResult(ok), nil
}

func parametersResult(result bool) *pgp.Parameters {
	return &pgp.Parameters{
		Parameter: []*pgp.Parameters_Parameter{{
			Name: &dtpb.String{Value: "result"},
			Value: &pgp.Parameters_Parameter_ValueX{
				Choice: &pgp.Parameters_Parameter_ValueX_Boolean{
					Boolean: &dtpb.Boolean{Value: result},
				},
			},
		}},
	}
}
