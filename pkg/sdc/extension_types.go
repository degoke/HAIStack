package sdc

// TemplateExtractContext references a contained resource template for extraction.
type TemplateExtractContext struct {
	TemplateReference string
	FullURL           string
	ResourceID        string
}

// TemplateExtractValue maps a FHIRPath expression or fixed value into a template path.
type TemplateExtractValue struct {
	Path       string
	Expression *Expression
	Value      any
	ValueType  string
}

// ContextExpression provides contextual resources to help answer a question.
type ContextExpression struct {
	Expression Expression
	Label      string
}

// ContextResourceResult holds evaluated resources for one contextExpression.
// An entry with only Label set means the query succeeded but returned no matches.
type ContextResourceResult struct {
	Label     string
	Resources []any
}

// ChoiceColumn guides multi-column rendering for choice/reference items.
type ChoiceColumn struct {
	Path       string
	Label      string
	ForDisplay bool
}

// CQFLibraryRef associates a CQL library with the questionnaire.
type CQFLibraryRef struct {
	LibraryCanonical string
	Name             string
}
