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
	resourceType string
	terminology  string
}

type isNode struct {
	nodeBase
	x      Node
	not    bool
	target string // "null" or type name
}

func identName(n Node) (string, bool) {
	id, ok := n.(*identNode)
	if !ok {
		return "", false
	}
	return id.name, true
}
