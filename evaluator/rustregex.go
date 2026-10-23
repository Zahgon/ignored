package evaluator

// The crate compiles every translated pattern with the Rust regex crate (regex 1.13.1 on
// regex-syntax 0.8.11) as a byte-oriented regular expression with Unicode mode off and a compiled
// size limit of 20000 bytes. This file reproduces that dialect: which texts it accepts, how it
// reports the ones it rejects, and what the accepted ones match.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	rustNestLimit = 250
	rustSizeLimit = 20000
)

type rxPos struct{ offset, line, column int }

type rxSpan struct{ start, end rxPos }

func (s rxSpan) less(o rxSpan) bool {
	if s.start.offset != o.start.offset {
		return s.start.offset < o.start.offset
	}
	return s.end.offset < o.end.offset
}

// rxError is a regex-syntax parse or translation error: a message and the span it points at.
type rxError struct {
	msg     string
	span    rxSpan
	aux     *rxSpan
	pattern string
}

// regexSyntaxText renders the error the way regex-syntax displays it.
func (e *rxError) regexSyntaxText() string {
	var b strings.Builder
	b.WriteString("regex parse error:\n")
	lines := strings.Split(e.pattern, "\n")
	if strings.HasSuffix(e.pattern, "\n") {
		lines = lines[:len(lines)-1]
	}
	multi := strings.Contains(e.pattern, "\n")
	lineCount := len(lines)
	if strings.HasSuffix(e.pattern, "\n") {
		lineCount++
	}
	width := 0
	if lineCount > 1 {
		width = len(strconv.Itoa(lineCount))
	}
	byLine := make([][]rxSpan, lineCount)
	var multiLine []rxSpan
	add := func(s rxSpan) {
		if s.start.line == s.end.line {
			i := s.start.line - 1
			byLine[i] = append(byLine[i], s)
			for j := len(byLine[i]) - 1; j > 0 && byLine[i][j].less(byLine[i][j-1]); j-- {
				byLine[i][j], byLine[i][j-1] = byLine[i][j-1], byLine[i][j]
			}
		} else {
			multiLine = append(multiLine, s)
		}
	}
	add(e.span)
	if e.aux != nil {
		add(*e.aux)
	}
	divider := strings.Repeat("~", 79)
	if multi {
		b.WriteString(divider + "\n")
	}
	for i, line := range lines {
		if width > 0 {
			n := strconv.Itoa(i + 1)
			b.WriteString(strings.Repeat(" ", width-len(n)) + n + ": ")
		} else {
			b.WriteString("    ")
		}
		b.WriteString(line + "\n")
		if spans := byLine[i]; len(spans) > 0 {
			pad := 4
			if width > 0 {
				pad = 2 + width
			}
			b.WriteString(strings.Repeat(" ", pad))
			pos := 0
			for _, s := range spans {
				for ; pos < s.start.column-1; pos++ {
					b.WriteByte(' ')
				}
				n := s.end.column - s.start.column
				if n < 1 {
					n = 1
				}
				b.WriteString(strings.Repeat("^", n))
				pos += n
			}
			b.WriteString("\n")
		}
	}
	if multi {
		b.WriteString(divider + "\n")
		var notes []string
		for _, s := range multiLine {
			notes = append(notes, fmt.Sprintf("on line %d (column %d) through line %d (column %d)",
				s.start.line, s.start.column, s.end.line, s.end.column-1))
		}
		if len(notes) > 0 {
			b.WriteString(strings.Join(notes, "\n") + "\n")
		}
	}
	b.WriteString("error: " + e.msg)
	return b.String()
}

const (
	errClassEscapeInvalid  = "invalid escape sequence found in character class"
	errClassRangeInvalid   = "invalid character class range, the start must be <= the end"
	errClassRangeLiteral   = "invalid range boundary, must be a literal"
	errClassUnclosed       = "unclosed character class"
	errDecimalEmpty        = "decimal literal empty"
	errDecimalInvalid      = "decimal literal invalid"
	errEscapeHexEmpty      = "hexadecimal literal empty"
	errEscapeHexInvalid    = "hexadecimal literal is not a Unicode scalar value"
	errEscapeHexDigit      = "invalid hexadecimal digit"
	errEscapeEOF           = "incomplete escape sequence, reached end of pattern prematurely"
	errEscapeUnrecognized  = "unrecognized escape sequence"
	errFlagDangling        = "dangling flag negation operator"
	errFlagDuplicate       = "duplicate flag"
	errFlagRepeatedNeg     = "flag negation operator repeated"
	errFlagEOF             = "expected flag but got end of regex"
	errFlagUnrecognized    = "unrecognized flag"
	errGroupNameDuplicate  = "duplicate capture group name"
	errGroupNameEmpty      = "empty capture group name"
	errGroupNameInvalid    = "invalid capture group character"
	errGroupNameEOF        = "unclosed capture group name"
	errGroupUnclosed       = "unclosed group"
	errGroupUnopened       = "unopened group"
	errRepCountInvalid     = "invalid repetition count range, the start must be <= the end"
	errRepCountDecimal     = "repetition quantifier expects a valid decimal"
	errRepCountUnclosed    = "unclosed counted repetition"
	errRepMissing          = "repetition operator missing expression"
	errSpecialWBUnclosed   = "special word boundary assertion is either unclosed or contains an invalid character"
	errSpecialWBUnknown    = "unrecognized special word boundary assertion, valid choices are: start, end, start-half or end-half"
	errSpecialWBOrRepEOF   = "found either the beginning of a special word boundary or a bounded repetition on a \\b with an opening brace, but no closing brace"
	errUnicodeClassInvalid = "invalid Unicode character class"
	errBackreference       = "backreferences are not supported"
	errLookAround          = "look-around, including look-ahead and look-behind, is not supported"
	errUnicodeNotAllowed   = "Unicode not allowed here"
)

func errNestLimit(limit int) string {
	return fmt.Sprintf("exceed the maximum number of nested parentheses/brackets (%d)", limit)
}

type astKind int

const (
	astEmpty astKind = iota
	astFlags
	astLiteral
	astDot
	astAssertion
	astClassUnicode
	astClassPerl
	astClassBracketed
	astRepetition
	astGroup
	astAlternation
	astConcat
)

type litKind int

const (
	litVerbatim litKind = iota
	litMeta
	litSuperfluous
	litOctal
	litHexFixedX
	litHexFixedOther
	litHexBrace
	litSpecial
)

const (
	assertStartLine = iota
	assertEndLine
	assertStartText
	assertEndText
	assertWordBoundary
	assertNotWordBoundary
	assertWordStartAngle
	assertWordEndAngle
	assertWordStart
	assertWordEnd
	assertWordStartHalf
	assertWordEndHalf
)

const (
	perlDigit = iota
	perlSpace
	perlWord
)

const (
	repZeroOrOne = iota
	repZeroOrMore
	repOneOrMore
	repRange
)

// rxNode is a regex-syntax AST node. Only the fields of its kind are set.
type rxNode struct {
	kind astKind
	span rxSpan

	c    rune
	lit  litKind
	asrt int

	perl    int
	negated bool

	bracket *rxBracketed

	repKind  int
	min, max int // max < 0: unbounded
	greedy   bool
	sub      *rxNode

	capture bool
	flags   []rxFlag

	asts []*rxNode
}

type rxFlag struct {
	span   rxSpan
	negate bool
	flag   rune
}

// byteValue mirrors ast::Literal::byte: only a two-digit \xNN escape is a raw byte.
func (n *rxNode) byteValue() (byte, bool) {
	if n.lit == litHexFixedX && n.c <= 0xFF {
		return byte(n.c), true
	}
	return 0, false
}

type itemKind int

const (
	itemEmpty itemKind = iota
	itemLiteral
	itemRange
	itemASCII
	itemUnicode
	itemPerl
	itemBracketed
	itemUnion
)

type rxItem struct {
	kind  itemKind
	span  rxSpan
	lit   *rxNode // literal, unicode or perl primitive
	start *rxNode
	end   *rxNode

	ascii   string
	negated bool

	bracket *rxBracketed
	items   []*rxItem
}

type opKind int

const (
	opIntersection opKind = iota
	opDifference
	opSymmetricDifference
)

type rxSet struct {
	item *rxItem // nil for a binary operation
	op   opKind
	span rxSpan
	lhs  *rxSet
	rhs  *rxSet
}

func (s *rxSet) setSpan() rxSpan {
	if s.item != nil {
		return s.item.span
	}
	return s.span
}

type rxBracketed struct {
	span    rxSpan
	negated bool
	set     *rxSet
}

type rxUnion struct {
	span  rxSpan
	items []*rxItem
}

func (u *rxUnion) push(it *rxItem) {
	if len(u.items) == 0 {
		u.span.start = it.span.start
	}
	u.span.end = it.span.end
	u.items = append(u.items, it)
}

func (u *rxUnion) intoItem() *rxItem {
	switch len(u.items) {
	case 0:
		return &rxItem{kind: itemEmpty, span: u.span}
	case 1:
		return u.items[0]
	}
	return &rxItem{kind: itemUnion, span: u.span, items: u.items}
}

type groupState struct {
	isAlt  bool
	alt    *rxNode // alternation being built
	concat *rxNode // concat before the group opened
	group  *rxNode
}

type classState struct {
	isOp  bool
	union *rxUnion
	set   *rxBracketed
	op    opKind
	lhs   *rxSet
}

type rxParser struct {
	pattern      string
	pos          rxPos
	stackGroup   []*groupState
	stackClass   []*classState
	captureIndex uint32
	captureNames []string
	captureSpans []rxSpan
}

type rxFail struct{ err *rxError }

func (p *rxParser) fail(span rxSpan, msg string) {
	panic(rxFail{&rxError{msg: msg, span: span, pattern: p.pattern}})
}

func (p *rxParser) failAux(span, aux rxSpan, msg string) {
	panic(rxFail{&rxError{msg: msg, span: span, aux: &aux, pattern: p.pattern}})
}

func (p *rxParser) eof() bool { return p.pos.offset == len(p.pattern) }

func (p *rxParser) char() rune {
	return p.charAt(p.pos.offset)
}

func (p *rxParser) charAt(i int) rune {
	r, _ := utf8.DecodeRuneInString(p.pattern[i:])
	return r
}

func (p *rxParser) bump() bool {
	if p.eof() {
		return false
	}
	c := p.char()
	if c == '\n' {
		p.pos.line++
		p.pos.column = 1
	} else {
		p.pos.column++
	}
	p.pos.offset += utf8.RuneLen(c)
	return !p.eof()
}

func (p *rxParser) bumpIf(prefix string) bool {
	if strings.HasPrefix(p.pattern[p.pos.offset:], prefix) {
		for range utf8.RuneCountInString(prefix) {
			p.bump()
		}
		return true
	}
	return false
}

func (p *rxParser) peek() (rune, bool) {
	if p.eof() {
		return 0, false
	}
	next := p.pos.offset + utf8.RuneLen(p.char())
	if next >= len(p.pattern) {
		return 0, false
	}
	return p.charAt(next), true
}

func (p *rxParser) span() rxSpan { return rxSpan{p.pos, p.pos} }

func (p *rxParser) spanChar() rxSpan {
	next := rxPos{offset: p.pos.offset + utf8.RuneLen(p.char()), line: p.pos.line, column: p.pos.column + 1}
	if p.char() == '\n' {
		next.line++
		next.column = 1
	}
	return rxSpan{p.pos, next}
}

func concatIntoAST(c *rxNode) *rxNode {
	switch len(c.asts) {
	case 0:
		return &rxNode{kind: astEmpty, span: c.span}
	case 1:
		return c.asts[0]
	}
	return c
}

func altIntoAST(a *rxNode) *rxNode {
	switch len(a.asts) {
	case 0:
		return &rxNode{kind: astEmpty, span: a.span}
	case 1:
		return a.asts[0]
	}
	return a
}

func newConcat(s rxSpan) *rxNode { return &rxNode{kind: astConcat, span: s} }

func (p *rxParser) pushAlternate(concat *rxNode) *rxNode {
	concat.span.end = p.pos
	if n := len(p.stackGroup); n > 0 && p.stackGroup[n-1].isAlt {
		alt := p.stackGroup[n-1].alt
		alt.asts = append(alt.asts, concatIntoAST(concat))
	} else {
		p.stackGroup = append(p.stackGroup, &groupState{isAlt: true, alt: &rxNode{
			kind: astAlternation, span: rxSpan{concat.span.start, p.pos}, asts: []*rxNode{concatIntoAST(concat)},
		}})
	}
	p.bump()
	return newConcat(p.span())
}

func (p *rxParser) pushGroup(concat *rxNode) *rxNode {
	group, setFlags := p.parseGroup()
	if setFlags != nil {
		concat.asts = append(concat.asts, setFlags)
		return concat
	}
	p.stackGroup = append(p.stackGroup, &groupState{concat: concat, group: group})
	return newConcat(p.span())
}

func (p *rxParser) popGroup(groupConcat *rxNode) *rxNode {
	var st *groupState
	var alt *rxNode
	n := len(p.stackGroup)
	if n == 0 {
		p.fail(p.spanChar(), errGroupUnopened)
	}
	top := p.stackGroup[n-1]
	p.stackGroup = p.stackGroup[:n-1]
	if top.isAlt {
		if n-1 == 0 || p.stackGroup[n-2].isAlt {
			p.fail(p.spanChar(), errGroupUnopened)
		}
		alt = top.alt
		st = p.stackGroup[n-2]
		p.stackGroup = p.stackGroup[:n-2]
	} else {
		st = top
	}
	groupConcat.span.end = p.pos
	p.bump()
	group := st.group
	group.span.end = p.pos
	if alt != nil {
		alt.span.end = groupConcat.span.end
		alt.asts = append(alt.asts, concatIntoAST(groupConcat))
		group.sub = altIntoAST(alt)
	} else {
		group.sub = concatIntoAST(groupConcat)
	}
	prior := st.concat
	prior.asts = append(prior.asts, group)
	return prior
}

func (p *rxParser) popGroupEnd(concat *rxNode) *rxNode {
	concat.span.end = p.pos
	var result *rxNode
	pop := func() *groupState {
		n := len(p.stackGroup)
		if n == 0 {
			return nil
		}
		st := p.stackGroup[n-1]
		p.stackGroup = p.stackGroup[:n-1]
		return st
	}
	st := pop()
	switch {
	case st == nil:
		result = concatIntoAST(concat)
	case st.isAlt:
		st.alt.span.end = p.pos
		st.alt.asts = append(st.alt.asts, concatIntoAST(concat))
		result = st.alt
	default:
		p.fail(st.group.span, errGroupUnclosed)
	}
	if st2 := pop(); st2 != nil {
		p.fail(st2.group.span, errGroupUnclosed)
	}
	return result
}

func (p *rxParser) pushClassOpen(parent *rxUnion) *rxUnion {
	set, union := p.parseSetClassOpen()
	p.stackClass = append(p.stackClass, &classState{union: parent, set: set})
	return union
}

func (p *rxParser) popClass(nested *rxUnion) (*rxUnion, *rxBracketed) {
	item := &rxSet{item: nested.intoItem()}
	prevset := p.popClassOp(item)
	n := len(p.stackClass)
	st := p.stackClass[n-1]
	p.stackClass = p.stackClass[:n-1]
	p.bump()
	st.set.span.end = p.pos
	st.set.set = prevset
	if len(p.stackClass) == 0 {
		return nil, st.set
	}
	st.union.push(&rxItem{kind: itemBracketed, span: st.set.span, bracket: st.set})
	return st.union, nil
}

func (p *rxParser) unclosedClassError() {
	for i := len(p.stackClass) - 1; i >= 0; i-- {
		if !p.stackClass[i].isOp {
			p.fail(p.stackClass[i].set.span, errClassUnclosed)
		}
	}
	panic("no open character class found")
}

func (p *rxParser) pushClassOp(kind opKind, next *rxUnion) *rxUnion {
	item := &rxSet{item: next.intoItem()}
	lhs := p.popClassOp(item)
	p.stackClass = append(p.stackClass, &classState{isOp: true, op: kind, lhs: lhs})
	return &rxUnion{span: p.span()}
}

func (p *rxParser) popClassOp(rhs *rxSet) *rxSet {
	n := len(p.stackClass)
	st := p.stackClass[n-1]
	if !st.isOp {
		return rhs
	}
	p.stackClass = p.stackClass[:n-1]
	return &rxSet{op: st.op, lhs: st.lhs, rhs: rhs, span: rxSpan{st.lhs.setSpan().start, rhs.setSpan().end}}
}

// parseRustRegex parses pattern as regex-syntax does (default options: octal off, nest limit
// 250) and returns its AST or the first error.
func parseRustRegex(pattern string) (ast *rxNode, err *rxError) {
	p := &rxParser{pattern: pattern, pos: rxPos{0, 1, 1}}
	defer func() {
		if r := recover(); r != nil {
			f, ok := r.(rxFail)
			if !ok {
				panic(r)
			}
			ast, err = nil, f.err
		}
	}()
	concat := newConcat(p.span())
	for !p.eof() {
		switch p.char() {
		case '(':
			concat = p.pushGroup(concat)
		case ')':
			concat = p.popGroup(concat)
		case '|':
			concat = p.pushAlternate(concat)
		case '[':
			concat.asts = append(concat.asts, &rxNode{kind: astClassBracketed, bracket: p.parseSetClass()})
			b := concat.asts[len(concat.asts)-1]
			b.span = b.bracket.span
			b.negated = b.bracket.negated
		case '?':
			concat = p.parseUncountedRepetition(concat, repZeroOrOne)
		case '*':
			concat = p.parseUncountedRepetition(concat, repZeroOrMore)
		case '+':
			concat = p.parseUncountedRepetition(concat, repOneOrMore)
		case '{':
			concat = p.parseCountedRepetition(concat)
		default:
			concat.asts = append(concat.asts, p.parsePrimitive())
		}
	}
	ast = p.popGroupEnd(concat)
	nestCheck(p, ast)
	return ast, nil
}

func (p *rxParser) parseUncountedRepetition(concat *rxNode, kind int) *rxNode {
	opStart := p.pos
	n := len(concat.asts)
	if n == 0 {
		p.fail(p.span(), errRepMissing)
	}
	sub := concat.asts[n-1]
	concat.asts = concat.asts[:n-1]
	if sub.kind == astEmpty || sub.kind == astFlags {
		p.fail(p.span(), errRepMissing)
	}
	greedy := true
	if p.bump() && p.char() == '?' {
		greedy = false
		p.bump()
	}
	_ = opStart
	concat.asts = append(concat.asts, &rxNode{kind: astRepetition, span: rxSpan{sub.span.start, p.pos},
		repKind: kind, greedy: greedy, sub: sub, max: -1})
	return concat
}

func (p *rxParser) parseDecimal() (int, *rxError) {
	for !p.eof() && isRustWhitespace(p.char()) {
		p.bump()
	}
	start := p.pos
	var digits strings.Builder
	for !p.eof() && p.char() >= '0' && p.char() <= '9' {
		digits.WriteRune(p.char())
		p.bump()
	}
	span := rxSpan{start, p.pos}
	for !p.eof() && isRustWhitespace(p.char()) {
		p.bump()
	}
	if digits.Len() == 0 {
		return 0, &rxError{msg: errDecimalEmpty, span: span, pattern: p.pattern}
	}
	v, err := strconv.ParseUint(digits.String(), 10, 32)
	if err != nil {
		return 0, &rxError{msg: errDecimalInvalid, span: span, pattern: p.pattern}
	}
	return int(v), nil
}

func specialize(e *rxError) *rxError {
	if e != nil && e.msg == errDecimalEmpty {
		e.msg = errRepCountDecimal
	}
	return e
}

func (p *rxParser) parseCountedRepetition(concat *rxNode) *rxNode {
	start := p.pos
	n := len(concat.asts)
	if n == 0 {
		p.fail(p.span(), errRepMissing)
	}
	sub := concat.asts[n-1]
	concat.asts = concat.asts[:n-1]
	if sub.kind == astEmpty || sub.kind == astFlags {
		p.fail(p.span(), errRepMissing)
	}
	if !p.bump() {
		p.fail(rxSpan{start, p.pos}, errRepCountUnclosed)
	}
	countStart, startErr := p.parseDecimal()
	startErr = specialize(startErr)
	if p.eof() {
		p.fail(rxSpan{start, p.pos}, errRepCountUnclosed)
	}
	lo, hi := 0, 0
	if p.char() == ',' {
		if !p.bump() {
			p.fail(rxSpan{start, p.pos}, errRepCountUnclosed)
		}
		if p.char() != '}' {
			if startErr != nil {
				panic(rxFail{startErr})
			}
			countEnd, endErr := p.parseDecimal()
			if endErr = specialize(endErr); endErr != nil {
				panic(rxFail{endErr})
			}
			lo, hi = countStart, countEnd
		} else {
			if startErr != nil {
				panic(rxFail{startErr})
			}
			lo, hi = countStart, -1
		}
	} else {
		if startErr != nil {
			panic(rxFail{startErr})
		}
		lo, hi = countStart, countStart
	}
	if p.eof() || p.char() != '}' {
		p.fail(rxSpan{start, p.pos}, errRepCountUnclosed)
	}
	greedy := true
	if p.bump() && p.char() == '?' {
		greedy = false
		p.bump()
	}
	opSpan := rxSpan{start, p.pos}
	if hi >= 0 && lo > hi {
		p.fail(opSpan, errRepCountInvalid)
	}
	concat.asts = append(concat.asts, &rxNode{kind: astRepetition, span: rxSpan{sub.span.start, p.pos},
		repKind: repRange, min: lo, max: hi, greedy: greedy, sub: sub})
	return concat
}

func (p *rxParser) parseGroup() (group *rxNode, setFlags *rxNode) {
	openSpan := p.spanChar()
	p.bump()
	if p.bumpIf("?=") || p.bumpIf("?!") || p.bumpIf("?<=") || p.bumpIf("?<!") {
		p.fail(rxSpan{openSpan.start, p.span().end}, errLookAround)
	}
	innerSpan := p.span()
	if p.bumpIf("?P<") || p.bumpIf("?<") {
		p.nextCaptureIndex()
		p.parseCaptureName()
		return &rxNode{kind: astGroup, span: openSpan, capture: true}, nil
	}
	if p.bumpIf("?") {
		if p.eof() {
			p.fail(openSpan, errGroupUnclosed)
		}
		flags := p.parseFlags()
		charEnd := p.char()
		p.bump()
		if charEnd == ')' {
			if len(flags) == 0 {
				p.fail(innerSpan, errRepMissing)
			}
			return nil, &rxNode{kind: astFlags, span: rxSpan{openSpan.start, p.pos}, flags: flags}
		}
		return &rxNode{kind: astGroup, span: openSpan, flags: flags}, nil
	}
	p.nextCaptureIndex()
	return &rxNode{kind: astGroup, span: openSpan, capture: true}, nil
}

func (p *rxParser) nextCaptureIndex() {
	p.captureIndex++
}

func isCaptureChar(c rune, first bool) bool {
	if first {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isRustAlphabetic(c)
	}
	return c == '_' || c == '.' || c == '[' || c == ']' || (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || isRustAlphanumeric(c)
}

func (p *rxParser) parseCaptureName() {
	if p.eof() {
		p.fail(p.span(), errGroupNameEOF)
	}
	start := p.pos
	for {
		if p.char() == '>' {
			break
		}
		if !isCaptureChar(p.char(), p.pos == start) {
			p.fail(p.spanChar(), errGroupNameInvalid)
		}
		if !p.bump() {
			break
		}
	}
	end := p.pos
	if p.eof() {
		p.fail(p.span(), errGroupNameEOF)
	}
	p.bump()
	name := p.pattern[start.offset:end.offset]
	if name == "" {
		p.fail(rxSpan{start, start}, errGroupNameEmpty)
	}
	span := rxSpan{start, end}
	for i, existing := range p.captureNames {
		if existing == name {
			p.failAux(span, p.captureSpans[i], errGroupNameDuplicate)
		}
	}
	p.captureNames = append(p.captureNames, name)
	p.captureSpans = append(p.captureSpans, span)
}

func (p *rxParser) parseFlags() []rxFlag {
	var flags []rxFlag
	var lastNegation *rxSpan
	for p.char() != ':' && p.char() != ')' {
		if p.char() == '-' {
			s := p.spanChar()
			lastNegation = &s
			for _, f := range flags {
				if f.negate {
					p.failAux(p.spanChar(), f.span, errFlagRepeatedNeg)
				}
			}
			flags = append(flags, rxFlag{span: s, negate: true})
		} else {
			lastNegation = nil
			c := p.char()
			if !strings.ContainsRune("imsUuRx", c) {
				p.fail(p.spanChar(), errFlagUnrecognized)
			}
			for _, f := range flags {
				if !f.negate && f.flag == c {
					p.failAux(p.spanChar(), f.span, errFlagDuplicate)
				}
			}
			flags = append(flags, rxFlag{span: p.spanChar(), flag: c})
		}
		if !p.bump() {
			p.fail(p.span(), errFlagEOF)
		}
	}
	if lastNegation != nil {
		p.fail(*lastNegation, errFlagDangling)
	}
	return flags
}

func (p *rxParser) parsePrimitive() *rxNode {
	switch c := p.char(); c {
	case '\\':
		return p.parseEscape()
	case '.':
		n := &rxNode{kind: astDot, span: p.spanChar()}
		p.bump()
		return n
	case '^':
		n := &rxNode{kind: astAssertion, span: p.spanChar(), asrt: assertStartLine}
		p.bump()
		return n
	case '$':
		n := &rxNode{kind: astAssertion, span: p.spanChar(), asrt: assertEndLine}
		p.bump()
		return n
	default:
		n := &rxNode{kind: astLiteral, span: p.spanChar(), lit: litVerbatim, c: c}
		p.bump()
		return n
	}
}

func isMetaCharacter(c rune) bool {
	return strings.ContainsRune(`\.+*?()|[]{}^$#&-~`, c)
}

func isEscapeableCharacter(c rune) bool {
	if isMetaCharacter(c) {
		return true
	}
	if c > 0x7F {
		return false
	}
	switch {
	case c >= '0' && c <= '9', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '<', c == '>':
		return false
	}
	return true
}

func (p *rxParser) parseEscape() *rxNode {
	start := p.pos
	if !p.bump() {
		p.fail(rxSpan{start, p.pos}, errEscapeEOF)
	}
	c := p.char()
	switch {
	case c >= '0' && c <= '9':
		p.fail(rxSpan{start, p.spanChar().end}, errBackreference)
	case c == 'x' || c == 'u' || c == 'U':
		lit := p.parseHex()
		lit.span.start = start
		return lit
	case c == 'p' || c == 'P':
		cls := p.parseUnicodeClass()
		cls.span.start = start
		return cls
	case strings.ContainsRune("dswDSW", c):
		cls := p.parsePerlClass()
		cls.span.start = start
		return cls
	}
	p.bump()
	span := rxSpan{start, p.pos}
	if isMetaCharacter(c) {
		return &rxNode{kind: astLiteral, span: span, lit: litMeta, c: c}
	}
	if isEscapeableCharacter(c) {
		return &rxNode{kind: astLiteral, span: span, lit: litSuperfluous, c: c}
	}
	special := map[rune]rune{'a': 0x07, 'f': 0x0C, 't': '\t', 'n': '\n', 'r': '\r', 'v': 0x0B}
	if v, ok := special[c]; ok {
		return &rxNode{kind: astLiteral, span: span, lit: litSpecial, c: v}
	}
	switch c {
	case 'A':
		return &rxNode{kind: astAssertion, span: span, asrt: assertStartText}
	case 'z':
		return &rxNode{kind: astAssertion, span: span, asrt: assertEndText}
	case 'b':
		wb := &rxNode{kind: astAssertion, span: span, asrt: assertWordBoundary}
		if !p.eof() && p.char() == '{' {
			if kind, ok := p.maybeParseSpecialWordBoundary(start); ok {
				wb.asrt = kind
				wb.span.end = p.pos
			}
		}
		return wb
	case 'B':
		return &rxNode{kind: astAssertion, span: span, asrt: assertNotWordBoundary}
	case '<':
		return &rxNode{kind: astAssertion, span: span, asrt: assertWordStartAngle}
	case '>':
		return &rxNode{kind: astAssertion, span: span, asrt: assertWordEndAngle}
	}
	p.fail(span, errEscapeUnrecognized)
	return nil
}

func (p *rxParser) maybeParseSpecialWordBoundary(wbStart rxPos) (int, bool) {
	isValid := func(c rune) bool { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '-' }
	start := p.pos
	if !p.bump() {
		p.fail(rxSpan{wbStart, p.pos}, errSpecialWBOrRepEOF)
	}
	startContents := p.pos
	if !isValid(p.char()) {
		p.pos = start
		return 0, false
	}
	var name strings.Builder
	for !p.eof() && isValid(p.char()) {
		name.WriteRune(p.char())
		p.bump()
	}
	if p.eof() || p.char() != '}' {
		p.fail(rxSpan{start, p.pos}, errSpecialWBUnclosed)
	}
	end := p.pos
	p.bump()
	switch name.String() {
	case "start":
		return assertWordStart, true
	case "end":
		return assertWordEnd, true
	case "start-half":
		return assertWordStartHalf, true
	case "end-half":
		return assertWordEndHalf, true
	}
	p.fail(rxSpan{startContents, end}, errSpecialWBUnknown)
	return 0, false
}

func isHex(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func validScalar(v uint64) bool {
	return v <= 0x10FFFF && !(v >= 0xD800 && v <= 0xDFFF)
}

func (p *rxParser) parseHex() *rxNode {
	digits := map[rune]int{'x': 2, 'u': 4, 'U': 8}[p.char()]
	isX := p.char() == 'x'
	if !p.bump() {
		p.fail(p.span(), errEscapeEOF)
	}
	if p.char() == '{' {
		return p.parseHexBrace()
	}
	start := p.pos
	var hex strings.Builder
	for i := 0; i < digits; i++ {
		if i > 0 && !p.bump() {
			p.fail(p.span(), errEscapeEOF)
		}
		if !isHex(p.char()) {
			p.fail(p.spanChar(), errEscapeHexDigit)
		}
		hex.WriteRune(p.char())
	}
	p.bump()
	end := p.pos
	v, err := strconv.ParseUint(hex.String(), 16, 32)
	if err != nil || !validScalar(v) {
		p.fail(rxSpan{start, end}, errEscapeHexInvalid)
	}
	kind := litHexFixedOther
	if isX {
		kind = litHexFixedX
	}
	return &rxNode{kind: astLiteral, span: rxSpan{start, end}, lit: kind, c: rune(v)}
}

func (p *rxParser) parseHexBrace() *rxNode {
	bracePos := p.pos
	start := p.spanChar().end
	var hex strings.Builder
	for p.bump() && p.char() != '}' {
		if !isHex(p.char()) {
			p.fail(p.spanChar(), errEscapeHexDigit)
		}
		hex.WriteRune(p.char())
	}
	if p.eof() {
		p.fail(rxSpan{bracePos, p.pos}, errEscapeEOF)
	}
	end := p.pos
	p.bump()
	if hex.Len() == 0 {
		p.fail(rxSpan{bracePos, p.pos}, errEscapeHexEmpty)
	}
	v, err := strconv.ParseUint(hex.String(), 16, 32)
	if err != nil || !validScalar(v) {
		p.fail(rxSpan{start, end}, errEscapeHexInvalid)
	}
	return &rxNode{kind: astLiteral, span: rxSpan{start, p.pos}, lit: litHexBrace, c: rune(v)}
}

func (p *rxParser) parseSetClass() *rxBracketed {
	union := &rxUnion{span: p.span()}
	for {
		if p.eof() {
			p.unclosedClassError()
		}
		switch p.char() {
		case '[':
			if len(p.stackClass) > 0 {
				if cls := p.maybeParseASCIIClass(); cls != nil {
					union.push(cls)
					continue
				}
			}
			union = p.pushClassOpen(union)
		case ']':
			nested, done := p.popClass(union)
			if done != nil {
				return done
			}
			union = nested
		case '&', '-', '~':
			c := p.char()
			if next, ok := p.peek(); ok && next == c {
				p.bump()
				p.bump()
				kind := map[rune]opKind{'&': opIntersection, '-': opDifference, '~': opSymmetricDifference}[c]
				union = p.pushClassOp(kind, union)
				continue
			}
			union.push(p.parseSetClassRange())
		default:
			union.push(p.parseSetClassRange())
		}
	}
}

func (p *rxParser) intoClassSetItem(prim *rxNode) *rxItem {
	switch prim.kind {
	case astLiteral:
		return &rxItem{kind: itemLiteral, span: prim.span, lit: prim}
	case astClassPerl:
		return &rxItem{kind: itemPerl, span: prim.span, lit: prim}
	case astClassUnicode:
		return &rxItem{kind: itemUnicode, span: prim.span, lit: prim}
	}
	p.fail(prim.span, errClassEscapeInvalid)
	return nil
}

func (p *rxParser) intoClassLiteral(prim *rxNode) *rxNode {
	if prim.kind != astLiteral {
		p.fail(prim.span, errClassRangeLiteral)
	}
	return prim
}

func (p *rxParser) parseSetClassRange() *rxItem {
	prim1 := p.parseSetClassItem()
	if p.eof() {
		p.unclosedClassError()
	}
	next, ok := p.peek()
	if p.char() != '-' || (ok && (next == ']' || next == '-')) {
		return p.intoClassSetItem(prim1)
	}
	if !p.bump() {
		p.unclosedClassError()
	}
	prim2 := p.parseSetClassItem()
	span := rxSpan{prim1.span.start, prim2.span.end}
	start := p.intoClassLiteral(prim1)
	end := p.intoClassLiteral(prim2)
	if start.c > end.c {
		p.fail(span, errClassRangeInvalid)
	}
	return &rxItem{kind: itemRange, span: span, start: start, end: end}
}

func (p *rxParser) parseSetClassItem() *rxNode {
	if p.char() == '\\' {
		return p.parseEscape()
	}
	n := &rxNode{kind: astLiteral, span: p.spanChar(), lit: litVerbatim, c: p.char()}
	p.bump()
	return n
}

func (p *rxParser) parseSetClassOpen() (*rxBracketed, *rxUnion) {
	start := p.pos
	if !p.bump() {
		p.fail(rxSpan{start, p.pos}, errClassUnclosed)
	}
	negated := false
	if p.char() == '^' {
		if !p.bump() {
			p.fail(rxSpan{start, p.pos}, errClassUnclosed)
		}
		negated = true
	}
	union := &rxUnion{span: p.span()}
	for p.char() == '-' {
		union.push(&rxItem{kind: itemLiteral, span: p.spanChar(),
			lit: &rxNode{kind: astLiteral, span: p.spanChar(), lit: litVerbatim, c: '-'}})
		if !p.bump() {
			p.fail(rxSpan{start, start}, errClassUnclosed)
		}
	}
	if len(union.items) == 0 && p.char() == ']' {
		union.push(&rxItem{kind: itemLiteral, span: p.spanChar(),
			lit: &rxNode{kind: astLiteral, span: p.spanChar(), lit: litVerbatim, c: ']'}})
		if !p.bump() {
			p.fail(rxSpan{start, p.pos}, errClassUnclosed)
		}
	}
	set := &rxBracketed{span: rxSpan{start, p.pos}, negated: negated,
		set: &rxSet{item: &rxItem{kind: itemUnion, span: rxSpan{union.span.start, union.span.start}}}}
	return set, union
}

var asciiClassNames = map[string]bool{
	"alnum": true, "alpha": true, "ascii": true, "blank": true, "cntrl": true, "digit": true, "graph": true,
	"lower": true, "print": true, "punct": true, "space": true, "upper": true, "word": true, "xdigit": true,
}

func (p *rxParser) maybeParseASCIIClass() *rxItem {
	start := p.pos
	negated := false
	if !p.bump() || p.char() != ':' {
		p.pos = start
		return nil
	}
	if !p.bump() {
		p.pos = start
		return nil
	}
	if p.char() == '^' {
		negated = true
		if !p.bump() {
			p.pos = start
			return nil
		}
	}
	nameStart := p.pos.offset
	for p.char() != ':' && p.bump() {
	}
	if p.eof() {
		p.pos = start
		return nil
	}
	name := p.pattern[nameStart:p.pos.offset]
	if !p.bumpIf(":]") {
		p.pos = start
		return nil
	}
	if !asciiClassNames[name] {
		p.pos = start
		return nil
	}
	return &rxItem{kind: itemASCII, span: rxSpan{start, p.pos}, ascii: name, negated: negated}
}

func (p *rxParser) parseUnicodeClass() *rxNode {
	negated := p.char() == 'P'
	if !p.bump() {
		p.fail(p.span(), errEscapeEOF)
	}
	var start rxPos
	if p.char() == '{' {
		start = p.spanChar().end
		for p.bump() && p.char() != '}' {
		}
		if p.eof() {
			p.fail(p.span(), errEscapeEOF)
		}
		p.bump()
	} else {
		start = p.pos
		if p.char() == '\\' {
			p.fail(p.spanChar(), errUnicodeClassInvalid)
		}
		p.bump()
	}
	return &rxNode{kind: astClassUnicode, span: rxSpan{start, p.pos}, negated: negated}
}

func (p *rxParser) parsePerlClass() *rxNode {
	c := p.char()
	span := p.spanChar()
	p.bump()
	kind := map[rune]int{'d': perlDigit, 'D': perlDigit, 's': perlSpace, 'S': perlSpace, 'w': perlWord, 'W': perlWord}[c]
	return &rxNode{kind: astClassPerl, span: span, perl: kind, negated: c >= 'A' && c <= 'Z'}
}

// nestCheck mirrors regex-syntax's NestLimiter, which visits the finished AST depth first.
func nestCheck(p *rxParser, root *rxNode) {
	depth := 0
	inc := func(s rxSpan) {
		if depth+1 > rustNestLimit {
			p.fail(s, errNestLimit(rustNestLimit))
		}
		depth++
	}
	var visitItem func(it *rxItem)
	var visitSet func(s *rxSet)
	visitSet = func(s *rxSet) {
		if s.item != nil {
			visitItem(s.item)
			return
		}
		inc(s.span)
		visitSet(s.lhs)
		visitSet(s.rhs)
		depth--
	}
	visitItem = func(it *rxItem) {
		switch it.kind {
		case itemBracketed:
			inc(it.span)
			visitSet(it.bracket.set)
			depth--
		case itemUnion:
			inc(it.span)
			for _, child := range it.items {
				visitItem(child)
			}
			depth--
		}
	}
	var visit func(n *rxNode)
	visit = func(n *rxNode) {
		switch n.kind {
		case astClassBracketed:
			inc(n.span)
			visitSet(n.bracket.set)
			depth--
		case astRepetition, astGroup:
			inc(n.span)
			if n.sub != nil {
				visit(n.sub)
			}
			depth--
		case astAlternation, astConcat:
			inc(n.span)
			for _, child := range n.asts {
				visit(child)
			}
			depth--
		}
	}
	visit(root)
}

// byteSet is a set of byte values.
type byteSet [4]uint64

func (s *byteSet) add(lo, hi int) {
	for b := lo; b <= hi; b++ {
		s[b>>6] |= 1 << (uint(b) & 63)
	}
}

func (s *byteSet) has(b int) bool { return s[b>>6]&(1<<(uint(b)&63)) != 0 }

func (s byteSet) union(o byteSet) byteSet {
	for i := range s {
		s[i] |= o[i]
	}
	return s
}

func (s byteSet) negate() byteSet {
	for i := range s {
		s[i] = ^s[i]
	}
	return s
}

var asciiClassRanges = map[string][][2]int{
	"alnum":  {{'0', '9'}, {'A', 'Z'}, {'a', 'z'}},
	"alpha":  {{'A', 'Z'}, {'a', 'z'}},
	"ascii":  {{0x00, 0x7F}},
	"blank":  {{'\t', '\t'}, {' ', ' '}},
	"cntrl":  {{0x00, 0x1F}, {0x7F, 0x7F}},
	"digit":  {{'0', '9'}},
	"graph":  {{'!', '~'}},
	"lower":  {{'a', 'z'}},
	"print":  {{' ', '~'}},
	"punct":  {{'!', '/'}, {':', '@'}, {'[', '`'}, {'{', '~'}},
	"space":  {{'\t', '\r'}, {' ', ' '}},
	"upper":  {{'A', 'Z'}},
	"word":   {{'0', '9'}, {'A', 'Z'}, {'_', '_'}, {'a', 'z'}},
	"xdigit": {{'0', '9'}, {'A', 'F'}, {'a', 'f'}},
}

func asciiSet(name string) byteSet {
	var s byteSet
	for _, r := range asciiClassRanges[name] {
		s.add(r[0], r[1])
	}
	return s
}

func perlSet(kind int, negated bool) byteSet {
	name := map[int]string{perlDigit: "digit", perlSpace: "space", perlWord: "word"}[kind]
	s := asciiSet(name)
	if negated {
		s = s.negate()
	}
	return s
}

// translator turns a parsed AST into a Go regexp over Latin-1 runes (one rune per input byte),
// raising the errors regex-syntax's translator raises with Unicode mode off and UTF-8 mode off.
type translator struct {
	pattern string
	out     strings.Builder
	size    int
}

func (t *translator) fail(span rxSpan, msg string) {
	panic(rxFail{&rxError{msg: msg, span: span, pattern: t.pattern}})
}

// literalBytes is the byte sequence a literal stands for outside a class.
func literalBytes(n *rxNode) []byte {
	if b, ok := n.byteValue(); ok && b > 0x7F {
		return []byte{b}
	}
	return []byte(string(n.c))
}

func (t *translator) classLiteralByte(n *rxNode) int {
	if b, ok := n.byteValue(); ok {
		return int(b)
	}
	if n.c > 0x7F {
		t.fail(n.span, errUnicodeNotAllowed)
	}
	return int(n.c)
}

func (t *translator) itemSet(it *rxItem) byteSet {
	var s byteSet
	switch it.kind {
	case itemLiteral:
		b := t.classLiteralByte(it.lit)
		s.add(b, b)
	case itemRange:
		lo := t.classLiteralByte(it.start)
		hi := t.classLiteralByte(it.end)
		s.add(lo, hi)
	case itemASCII:
		s = asciiSet(it.ascii)
		if it.negated {
			s = s.negate()
		}
	case itemUnicode:
		t.fail(it.lit.span, errUnicodeNotAllowed)
	case itemPerl:
		s = perlSet(it.lit.perl, it.lit.negated)
	case itemBracketed:
		s = t.bracketSet(it.bracket)
	case itemUnion:
		for _, child := range it.items {
			s = s.union(t.itemSet(child))
		}
	}
	return s
}

func (t *translator) setOf(set *rxSet) byteSet {
	if set.item != nil {
		return t.itemSet(set.item)
	}
	lhs := t.setOf(set.lhs)
	rhs := t.setOf(set.rhs)
	var out byteSet
	for i := range out {
		switch set.op {
		case opIntersection:
			out[i] = lhs[i] & rhs[i]
		case opDifference:
			out[i] = lhs[i] &^ rhs[i]
		case opSymmetricDifference:
			out[i] = lhs[i] ^ rhs[i]
		}
	}
	return out
}

func (t *translator) bracketSet(b *rxBracketed) byteSet {
	s := t.setOf(b.set)
	if b.negated {
		s = s.negate()
	}
	return s
}

func writeRune(b *strings.Builder, v int) {
	fmt.Fprintf(b, `\x{%x}`, v)
}

func (t *translator) writeSet(s byteSet) {
	t.size += classCost(s)
	var ranges [][2]int
	for b := 0; b < 256; b++ {
		if !s.has(b) {
			continue
		}
		if n := len(ranges); n > 0 && ranges[n-1][1] == b-1 {
			ranges[n-1][1] = b
		} else {
			ranges = append(ranges, [2]int{b, b})
		}
	}
	if len(ranges) == 0 {
		t.out.WriteString(`[^\x00-\x{10FFFF}]`)
		return
	}
	t.out.WriteByte('[')
	for _, r := range ranges {
		writeRune(&t.out, r[0])
		if r[1] != r[0] {
			t.out.WriteByte('-')
			writeRune(&t.out, r[1])
		}
	}
	t.out.WriteByte(']')
}

func (t *translator) node(n *rxNode) {
	switch n.kind {
	case astEmpty, astFlags:
	case astLiteral:
		for _, b := range literalBytes(n) {
			t.size += sizeLiteralByte
			writeRune(&t.out, int(b))
		}
	case astDot:
		var s byteSet
		s.add(0, 255)
		s[0] &^= 1 << '\n'
		t.writeSet(s)
	case astAssertion:
		t.size += sizeAssertion
		switch n.asrt {
		case assertStartLine, assertStartText:
			t.out.WriteString(`\A`)
		case assertEndLine, assertEndText:
			t.out.WriteString(`\z`)
		case assertWordBoundary:
			t.out.WriteString(`\b`)
		case assertNotWordBoundary:
			t.out.WriteString(`\B`)
		default:
			// The remaining word assertions never come out of a translated gitignore pattern.
			t.out.WriteString(`\b`)
		}
	case astClassUnicode:
		t.fail(n.span, errUnicodeNotAllowed)
	case astClassPerl:
		t.writeSet(perlSet(n.perl, n.negated))
	case astClassBracketed:
		t.writeSet(t.bracketSet(n.bracket))
	case astRepetition:
		t.out.WriteString("(?:")
		before := t.size
		t.node(n.sub)
		inner := t.size - before
		t.out.WriteString(")")
		switch n.repKind {
		case repZeroOrOne:
			t.out.WriteString("?")
			t.size += sizeRepetition
		case repZeroOrMore:
			t.out.WriteString("*")
			t.size += sizeRepetition
		case repOneOrMore:
			t.out.WriteString("+")
			t.size += inner + sizeRepetition
		case repRange:
			if n.max < 0 {
				fmt.Fprintf(&t.out, "{%d,}", n.min)
			} else {
				fmt.Fprintf(&t.out, "{%d,%d}", n.min, n.max)
			}
			copies := n.min
			if n.max > n.min {
				copies = n.max
			}
			t.size += inner*(copies-1) + sizeRepetition
		}
		if !n.greedy {
			t.out.WriteString("?")
		}
	case astGroup:
		t.out.WriteString("(?:")
		t.node(n.sub)
		t.out.WriteString(")")
		if n.capture {
			t.size += sizeCapture
		}
	case astAlternation:
		t.out.WriteString("(?:")
		for i, child := range n.asts {
			if i > 0 {
				t.out.WriteString("|")
				t.size += sizeAlternate
			}
			t.node(child)
		}
		t.out.WriteString(")")
	case astConcat:
		for _, child := range n.asts {
			t.node(child)
		}
	}
}

// Compiled-size model. The crate builds every glob with a size limit of 20000 bytes on the
// compiled program. The model below charges each construct its share of that program; it is
// calibrated against the reference so that translated patterns with at most a few dozen
// wildcard, bracket or literal tokens stay well inside the limit and patterns with hundreds of
// them exceed it.
const (
	sizeLiteralByte = 33
	sizeAssertion   = 16
	sizeRepetition  = 16
	sizeCapture     = 32
	sizeAlternate   = 16
	sizeBase        = 400
)

func classCost(s byteSet) int {
	runs := 0
	for b := 0; b < 256; b++ {
		if s.has(b) && (b == 0 || !s.has(b-1)) {
			runs++
		}
	}
	return 40 + 8*runs
}

// rustRegex is a compiled glob regex: the Rust text and a Go matcher with the same byte
// semantics.
type rustRegex struct {
	text string
	re   *regexp.Regexp
}

// errCompiledTooBig marks a pattern rejected by the compiled-size limit.
type rustRegexError struct {
	syntax *rxError
	tooBig bool
}

func (e *rustRegexError) Error() string {
	if e.tooBig {
		return fmt.Sprintf("Compiled regex exceeds size limit of %d bytes.", rustSizeLimit)
	}
	return e.syntax.regexSyntaxText()
}

// debug renders the regex error the way the regex crate's Debug implementation does.
func (e *rustRegexError) debug() string {
	if e.tooBig {
		return fmt.Sprintf("CompiledTooBig(%d)", rustSizeLimit)
	}
	divider := strings.Repeat("~", 79)
	return "Syntax(\n" + divider + "\n" + e.syntax.regexSyntaxText() + "\n" + divider + "\n)"
}

func compileRustRegex(text string) (rx *rustRegex, err *rustRegexError) {
	ast, perr := parseRustRegex(text)
	if perr != nil {
		return nil, &rustRegexError{syntax: perr}
	}
	t := &translator{pattern: text}
	func() {
		defer func() {
			if r := recover(); r != nil {
				f, ok := r.(rxFail)
				if !ok {
					panic(r)
				}
				err = &rustRegexError{syntax: f.err}
			}
		}()
		t.node(ast)
	}()
	if err != nil {
		return nil, err
	}
	if t.size+sizeBase > rustSizeLimit {
		return nil, &rustRegexError{tooBig: true}
	}
	re, cerr := regexp.Compile(t.out.String())
	if cerr != nil {
		panic("evaluator: translated regex does not compile: " + cerr.Error())
	}
	return &rustRegex{text: text, re: re}, nil
}

// match reports whether the regex matches the bytes of s.
func (r *rustRegex) match(s string) bool {
	runes := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		runes[i] = rune(s[i])
	}
	return r.re.MatchString(string(runes))
}

func isRustWhitespace(c rune) bool {
	switch c {
	case '\t', '\n', 0x0B, 0x0C, '\r', ' ', 0x85, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return c >= 0x2000 && c <= 0x200A
}

func isRustAlphabetic(c rune) bool { return unicode.IsLetter(c) }

func isRustAlphanumeric(c rune) bool { return unicode.IsLetter(c) || unicode.IsNumber(c) }
