package sdc

import (
	"errors"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func questionnaireResolveError(canonical string, err error) error {
	if errors.Is(err, types.ErrQuestionnaireNotFound) {
		return types.NewQuestionnaireNotFoundError(canonical, err)
	}
	return types.NewQuestionnaireResolutionFailedError(err)
}
