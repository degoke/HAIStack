package cql

// Node is a CQL expression AST node.
type Node interface {
	node()
	Source() string
}

type nodeBase struct{ src string }

func (n nodeBase) node()          {}
func (n nodeBase) Source() string { return n.src }

type litNode struct {
	nodeBase
	value any
}

type identNode struct {
	nodeBase
	name   string
	quoted bool
}

type binaryNode struct {
	nodeBase
	op          string
	left, right Node
}

type unaryNode struct {
	nodeBase
	op string
	x  Node
}

type memberNode struct {
	nodeBase
	x    Node
	name string
}

type callNode struct {
	nodeBase
	callee Node
	args   []Node
}

type listNode struct {
	nodeBase
	elems []Node
}

type tupleNode struct {
	nodeBase
	fields []tupleField
}

type tupleField struct {
	name  string
	value Node
}

type ifNode struct {
	nodeBase
	cond, thenN, elseN Node
}

type retrieveNode struct {
	nodeBase
	resourceType       string
	terminology        string
	comparator         string
	codePath           string
	datePath           string
	dateLow            Node
	dateHigh           Node
	dateLowClosed      bool
	dateHighClosed     bool
	dateLowClosedExpr  Node
	dateHighClosedExpr Node
}

type isNode struct {
	nodeBase
	x      Node
	not    bool
	target string // "null" or type name
}

type quantityNode struct {
	nodeBase
	value any
	unit  string
}

type intervalNode struct {
	nodeBase
	low, high      Node
	lowClosed      bool
	highClosed     bool
	lowClosedExpr  Node
	highClosedExpr Node
}

type betweenNode struct {
	nodeBase
	x, low, high Node
}

type durationNode struct {
	nodeBase
	unit       string
	of         Node
	left       Node
	right      Node
	difference bool
}

type caseNode struct {
	nodeBase
	test  Node
	whens []caseWhen
	elseN Node
}

type caseWhen struct {
	when, then Node
}

type queryNode struct {
	nodeBase
	sources  []querySource
	lets     []letClause
	related  []relatedClause
	where    Node
	ret      Node
	distinct bool
	sort     []sortItem
	agg      *aggregateNode
}

type querySource struct {
	expr  Node
	alias string
}

type letClause struct {
	name string
	expr Node
}

type relatedClause struct {
	source  querySource
	such    Node
	without bool
}

type sortItem struct {
	expr Node
	desc bool
}

type indexNode struct {
	nodeBase
	x     Node
	index Node
}

type convertNode struct {
	nodeBase
	x      Node
	target string
}

type codeLitNode struct {
	nodeBase
	code    string
	system  string
	display string
}

type aggregateNode struct {
	nodeBase
	name     string
	starting Node
	body     Node
	distinct bool
}

func identName(n Node) (string, bool) {
	id, ok := n.(*identNode)
	if !ok {
		return "", false
	}
	return id.name, true
}
