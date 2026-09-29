package sessel

import (
	"sync"

	"github.com/sky-valley/pagelike/internal/selector"
)

// node is an AST node. Every node records its program offset for messages.
type node interface{ at() int }

type base struct{ pos int }

func (b base) at() int { return b.pos }

type (
	litNode struct {
		base
		val Value
	}
	strNode struct {
		base
		parts []strNodePart
	}
	identNode struct {
		base
		name string
	}
	listNode struct {
		base
		items []node
	}
	dictNode struct {
		base
		keys []string
		vals []node
	}
	unaryNode struct {
		base
		op tokKind
		x  node
	}
	binaryNode struct {
		base
		op   tokKind
		l, r node
	}
	ternaryNode struct {
		base
		c, a, b node
	}
	isaNode struct {
		base
		x    node
		path []string
	}
	memberNode struct {
		base
		x    node
		name string
		opt  bool
	}
	methodNode struct {
		base
		x    node
		name string
		args []node
		opt  bool
	}
	indexNode struct {
		base
		x, idx node
		opt    bool
	}
	subSelectNode struct {
		base
		x   node
		sel *selTemplate
	}
	callNode struct {
		base
		name string
		args []node
	}
	selectorNode struct {
		base
		sel     *selTemplate
		sources []node // nil: default scope
		hasFrom bool
	}
	priorNode     struct{ base }
	fromBlockNode struct {
		base
		sources []node
		body    *blockNode
	}
	blockNode struct {
		base
		stmts []node
	}
	letNode struct {
		base
		pat pattern
		val node
	}
	assignNode struct {
		base
		target node // memberNode chain on an identifier, or indexNode on an identifier
		val    node
	}
	lambdaNode struct {
		base
		params []string
		rest   string
		body   node
	}
	ifNode struct {
		base
		conds  []node
		blocks []*blockNode
		els    *blockNode
	}
	tryNode struct {
		base
		body  *blockNode
		name  string
		catch *blockNode
	}
	returnNode struct {
		base
		x node // nil: return null
	}
	throwNode struct {
		base
		x node
	}
	newNode struct {
		base
		prefix  string // namespace prefix of p|tag
		name    string
		mods    []modifier
		items   []citem
		hasBody bool
	}
)

type strNodePart struct {
	lit  string
	expr node
}

// pattern is a let/destructuring target.
type pattern struct {
	name   string     // plain binding
	fields []patField // { a, b: c }
	items  []string   // [x, y]
	rest   string     // [x, ...rest]
	isDict bool
	isList bool
}

type patField struct{ key, name string }

// modifier is a construction modifier: .class, #id or [attr=value].
type modifier struct {
	kind   byte // '.', '#', '['
	name   string
	prefix string // attribute namespace prefix ([p|name])
	hasVal bool
	lit    string // quoted literal value
	hole   *selHole
}

// citem is a construction body item.
type citem struct {
	name string // "" positional, "text" text, other: named property
	expr node
}

// program is a compiled Sessel program.
type program struct {
	decls []decl
	body  *blockNode
	src   string
}

type decl struct {
	kind string // "schema" or "namespace"
	name string
	url  string
}

// selTemplate is a selector literal with its embedded expressions
// (R-SESSEL-202): literal CSS chunks and holes.
type selTemplate struct {
	src   string
	parts []selPart
	holes int
	isa   bool   // uses :isa() (compiled per host)
	funcs bool   // uses selector functions (expanded per evaluation)
	nsErr string // an undeclared namespace prefix (TypeError when evaluated)

	once     sync.Once
	compiled *selector.Selector
	cerr     error
}

type selPart struct {
	lit  string
	hole *selHole
}

// selHole is an unquoted value inside a selector or construction attribute.
type selHole struct {
	raw    string
	expr   node   // nil when raw does not parse as an expression
	ident  string // raw is a single identifier
	number bool   // raw is a CSS number or dimension
	flag   string // trailing attribute flag (" i" / " s") split off raw
}
