package main

import (
	"strings"
	"sync"
)

type Block struct {
	ID        string
	Kind      string
	Name      string
	Header    string
	Body      []string
	StartLine int
	EndLine   int
	Section   string
}

func (b Block) Source() string { return strings.Join(append([]string{b.Header}, b.Body...), "\n") }

type RuleInfo struct {
	Block      Block
	OnEvent    string
	Every      string
	WhenLines  []string
	DoLines    []string
	ThenLines  []string
	CatchLines []string
	Reads      []string
	Writes     []string
	Functions  []string
}

type ActionInfo struct {
	Name        string
	Declared    bool
	Block       Block
	CalledBy    []string
	CallForms   []string
	Inputs      []string
	Outputs     []string
	Writes      []string
	SafetyHints []string
}

type CondEval struct {
	Condition string
	Status    string
	Why       string
}

type SimWrite struct {
	Target string
	Value  string
}

type SimStep struct {
	Index      int
	Phase      string
	Rule       string
	Verdict    string
	Conditions []CondEval
	Actions    []string
	Writes     []SimWrite
	Note       string
}

type SimKV struct{ Key, Value string }

type SimulationReport struct {
	Event      string
	Steps      []SimStep
	FinalState []SimKV
}

type GraphNode struct {
	ID       string
	Kind     string
	Label    string
	Detail   string
	Layer    int
	X        int
	Y        int
	URL      string
	Status   string
	Selected bool
	Focused  bool
}

type GraphEdge struct {
	ID     string
	From   string
	To     string
	Kind   string
	Label  string
	Active bool
}

type GraphCluster struct {
	ID    string
	Label string
	Kind  string
	Nodes []string
}

type GraphTimelineItem struct {
	NodeID string
	Kind   string
	Label  string
	Detail string
	Status string
	URL    string
}

type StateFieldInfo struct {
	Name        string
	ReadBy      []string
	WrittenBy   []string
	ProtectedBy []string
}

type ProjectGraph struct {
	Nodes       []GraphNode
	Edges       []GraphEdge
	Clusters    []GraphCluster
	Timeline    []GraphTimelineItem
	StateFields []StateFieldInfo
	Filter      string
	Focus       string
	Selected    string
	Width       int
	Height      int
}

type ProjectModel struct {
	Path                string
	Source              string
	Format              string
	NormalizedSource    string
	CompileOK           bool
	SystemName          string
	Blocks              []Block
	Rules               map[string]RuleInfo
	States              map[string]Block
	Events              map[string]Block
	Conditions          map[string]Block
	Always              map[string]Block
	Views               map[string]Block
	Functions           map[string]Block
	Actions             map[string]ActionInfo
	InferredFunctions   []string
	Diagnostics         []string
	CompilerDiagnostics []CompilerDiagnostic
	Sections            []SectionGroup
	Graph               ProjectGraph
}

type CompilerDiagnostic struct {
	Code    string
	Kind    string
	Entity  string
	Line    int
	Message string
	Hint    string
}

type SectionGroup struct {
	Name   string
	Blocks []Block
}

type AppState struct {
	mu          sync.Mutex
	source      string
	path        string
	msg         string
	model       ProjectModel
	modelSource string
	modelPath   string
	modelValid  bool
}

var state = &AppState{}
