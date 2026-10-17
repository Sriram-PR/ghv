package read

import (
	"errors"
	"fmt"
)

const maxGraphQLDepth = 64

// classifyQuery parses doc as a GraphQL executable document (spec section 2)
// and accepts it only if every definition is a query operation or a fragment.
func classifyQuery(doc string) error {
	toks, err := lexGraphQL(doc)
	if err != nil {
		return err
	}
	p := &gqlParser{toks: toks}
	if p.peek().kind == tokEOF {
		return errors.New("graphql: empty document")
	}
	for p.peek().kind != tokEOF {
		if err := p.definition(); err != nil {
			return err
		}
	}
	return nil
}

type gqlParser struct {
	toks  []token
	i     int
	depth int
}

func (p *gqlParser) peek() token { return p.toks[p.i] }

func (p *gqlParser) next() token {
	t := p.toks[p.i]
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

func (p *gqlParser) isPunct(s string) bool {
	t := p.peek()
	return t.kind == tokPunct && t.text == s
}

func (p *gqlParser) isName(s string) bool {
	t := p.peek()
	return t.kind == tokName && t.text == s
}

func (p *gqlParser) errorf(format string, args ...any) error {
	return fmt.Errorf("graphql: %s at %d", fmt.Sprintf(format, args...), p.peek().pos)
}

func (p *gqlParser) expectPunct(s string) error {
	if !p.isPunct(s) {
		return p.errorf("expected %q", s)
	}
	p.next()
	return nil
}

func (p *gqlParser) expectName() (string, error) {
	t := p.peek()
	if t.kind != tokName {
		return "", p.errorf("expected a name")
	}
	p.next()
	return t.text, nil
}

func (p *gqlParser) enter() error {
	p.depth++
	if p.depth > maxGraphQLDepth {
		return p.errorf("nested deeper than %d", maxGraphQLDepth)
	}
	return nil
}

func (p *gqlParser) leave() { p.depth-- }

func (p *gqlParser) definition() error {
	switch {
	case p.isPunct("{"):
		return p.selectionSet()
	case p.isName("query"):
		p.next()
		if p.peek().kind == tokName {
			p.next()
		}
		if p.isPunct("(") {
			if err := p.variableDefinitions(); err != nil {
				return err
			}
		}
		if err := p.directives(); err != nil {
			return err
		}
		return p.selectionSet()
	case p.isName("fragment"):
		p.next()
		name, err := p.expectName()
		if err != nil {
			return err
		}
		if name == "on" {
			return p.errorf("fragment cannot be named \"on\"")
		}
		if !p.isName("on") {
			return p.errorf("expected \"on\"")
		}
		p.next()
		if _, err := p.expectName(); err != nil {
			return err
		}
		if err := p.directives(); err != nil {
			return err
		}
		return p.selectionSet()
	}
	return p.errorf("definition starting %q is not a query", p.peek().text)
}

func (p *gqlParser) selectionSet() error {
	if err := p.enter(); err != nil {
		return err
	}
	defer p.leave()
	if err := p.expectPunct("{"); err != nil {
		return err
	}
	for {
		if err := p.selection(); err != nil {
			return err
		}
		if p.isPunct("}") {
			p.next()
			return nil
		}
	}
}

func (p *gqlParser) selection() error {
	if p.isPunct("...") {
		p.next()
		switch {
		case p.isName("on"):
			p.next()
			if _, err := p.expectName(); err != nil {
				return err
			}
		case p.peek().kind == tokName:
			p.next()
			return p.directives()
		}
		if err := p.directives(); err != nil {
			return err
		}
		return p.selectionSet()
	}
	if _, err := p.expectName(); err != nil {
		return err
	}
	if p.isPunct(":") {
		p.next()
		if _, err := p.expectName(); err != nil {
			return err
		}
	}
	if p.isPunct("(") {
		if err := p.arguments(); err != nil {
			return err
		}
	}
	if err := p.directives(); err != nil {
		return err
	}
	if p.isPunct("{") {
		return p.selectionSet()
	}
	return nil
}

func (p *gqlParser) arguments() error {
	p.next()
	for {
		if _, err := p.expectName(); err != nil {
			return err
		}
		if err := p.expectPunct(":"); err != nil {
			return err
		}
		if err := p.value(); err != nil {
			return err
		}
		if p.isPunct(")") {
			p.next()
			return nil
		}
	}
}

func (p *gqlParser) directives() error {
	for p.isPunct("@") {
		p.next()
		if _, err := p.expectName(); err != nil {
			return err
		}
		if p.isPunct("(") {
			if err := p.arguments(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *gqlParser) variableDefinitions() error {
	p.next()
	for {
		if err := p.expectPunct("$"); err != nil {
			return err
		}
		if _, err := p.expectName(); err != nil {
			return err
		}
		if err := p.expectPunct(":"); err != nil {
			return err
		}
		if err := p.typeRef(); err != nil {
			return err
		}
		if p.isPunct("=") {
			p.next()
			if err := p.value(); err != nil {
				return err
			}
		}
		if err := p.directives(); err != nil {
			return err
		}
		if p.isPunct(")") {
			p.next()
			return nil
		}
	}
}

func (p *gqlParser) typeRef() error {
	if err := p.enter(); err != nil {
		return err
	}
	defer p.leave()
	if p.isPunct("[") {
		p.next()
		if err := p.typeRef(); err != nil {
			return err
		}
		if err := p.expectPunct("]"); err != nil {
			return err
		}
	} else if _, err := p.expectName(); err != nil {
		return err
	}
	if p.isPunct("!") {
		p.next()
	}
	return nil
}

func (p *gqlParser) value() error {
	if err := p.enter(); err != nil {
		return err
	}
	defer p.leave()
	t := p.peek()
	switch {
	case p.isPunct("$"):
		p.next()
		_, err := p.expectName()
		return err
	case t.kind == tokInt || t.kind == tokFloat || t.kind == tokString || t.kind == tokBlockString || t.kind == tokName:
		p.next()
		return nil
	case p.isPunct("["):
		p.next()
		for !p.isPunct("]") {
			if err := p.value(); err != nil {
				return err
			}
		}
		p.next()
		return nil
	case p.isPunct("{"):
		p.next()
		for !p.isPunct("}") {
			if _, err := p.expectName(); err != nil {
				return err
			}
			if err := p.expectPunct(":"); err != nil {
				return err
			}
			if err := p.value(); err != nil {
				return err
			}
		}
		p.next()
		return nil
	}
	return p.errorf("expected a value")
}
