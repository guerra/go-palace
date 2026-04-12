// Package graph builds a navigable graph over the palace structure.
// Nodes are rooms, edges are shared rooms across wings (tunnels).
// Ports palace_graph.py.
package graph

import (
	"fmt"
	"sort"
	"strings"

	"go-palace/pkg/palace"
)

// Node represents a room in the palace graph.
type Node struct {
	Room  string
	Wings []string
	Halls []string
	Count int
	Dates []string
}

// Edge represents a connection between two wings through a shared room.
type Edge struct {
	Room  string
	WingA string
	WingB string
	Hall  string
	Count int
}

// TraversalResult is one hop in a BFS traversal.
type TraversalResult struct {
	Room         string
	Wings        []string
	Halls        []string
	Count        int
	Hop          int
	ConnectedVia []string
}

// TunnelResult is a room that spans multiple wings.
type TunnelResult struct {
	Room   string
	Wings  []string
	Halls  []string
	Count  int
	Recent string
}

// GraphStats summarises the palace graph.
type GraphStats struct {
	TotalRooms   int
	TunnelRooms  int
	TotalEdges   int
	RoomsPerWing map[string]int
	TopTunnels   []TunnelResult
}

// TraverseError is returned when the start room is not found.
type TraverseError struct {
	Message     string
	Suggestions []string
}

func (e *TraverseError) Error() string { return e.Message }

// BuildGraph fetches all drawers in batches and builds the room graph.
func BuildGraph(p *palace.Palace) (map[string]Node, []Edge, error) {
	roomData := map[string]*roomAgg{}

	offset := 0
	for {
		drawers, err := p.Get(palace.GetOptions{Limit: 1000, Offset: offset})
		if err != nil {
			return nil, nil, fmt.Errorf("graph: get batch at offset %d: %w", offset, err)
		}
		if len(drawers) == 0 {
			break
		}
		for _, d := range drawers {
			room := d.Room
			wing := d.Wing
			if room == "" || room == "general" || wing == "" {
				continue
			}
			agg, ok := roomData[room]
			if !ok {
				agg = &roomAgg{
					wings: map[string]bool{},
					halls: map[string]bool{},
					dates: map[string]bool{},
				}
				roomData[room] = agg
			}
			agg.wings[wing] = true
			hall := ""
			if h, ok := d.Metadata["hall"]; ok {
				if hs, ok := h.(string); ok {
					hall = hs
				}
			}
			if hall != "" {
				agg.halls[hall] = true
			}
			date := d.FiledAt.Format("2006-01-02")
			if date != "0001-01-01" {
				agg.dates[date] = true
			}
			agg.count++
		}
		offset += len(drawers)
	}

	// Build nodes
	nodes := make(map[string]Node, len(roomData))
	for room, agg := range roomData {
		wings := sortedKeys(agg.wings)
		halls := sortedKeys(agg.halls)
		dates := sortedKeys(agg.dates)
		if len(dates) > 5 {
			dates = dates[len(dates)-5:]
		}
		nodes[room] = Node{
			Room:  room,
			Wings: wings,
			Halls: halls,
			Count: agg.count,
			Dates: dates,
		}
	}

	// Build edges: rooms with 2+ wings
	var edges []Edge
	for room, node := range nodes {
		if len(node.Wings) < 2 {
			continue
		}
		for i, wa := range node.Wings {
			for _, wb := range node.Wings[i+1:] {
				for _, hall := range node.Halls {
					edges = append(edges, Edge{Room: room, WingA: wa, WingB: wb, Hall: hall, Count: node.Count})
				}
			}
		}
	}

	return nodes, edges, nil
}

// Traverse does a BFS from startRoom, finding connected rooms through shared wings.
func Traverse(p *palace.Palace, startRoom string, maxHops int) ([]TraversalResult, error) {
	nodes, _, err := BuildGraph(p)
	if err != nil {
		return nil, err
	}

	start, ok := nodes[startRoom]
	if !ok {
		return nil, &TraverseError{
			Message:     fmt.Sprintf("room %q not found", startRoom),
			Suggestions: fuzzyMatch(startRoom, nodes, 5),
		}
	}

	visited := map[string]bool{startRoom: true}
	results := []TraversalResult{{
		Room:  startRoom,
		Wings: start.Wings,
		Halls: start.Halls,
		Count: start.Count,
		Hop:   0,
	}}

	type frontier struct {
		room  string
		depth int
	}
	queue := []frontier{{startRoom, 0}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= maxHops {
			continue
		}
		current, ok := nodes[cur.room]
		if !ok {
			continue
		}
		currentWings := toSet(current.Wings)

		for room, data := range nodes {
			if visited[room] {
				continue
			}
			shared := intersect(currentWings, toSet(data.Wings))
			if len(shared) > 0 {
				visited[room] = true
				results = append(results, TraversalResult{
					Room:         room,
					Wings:        data.Wings,
					Halls:        data.Halls,
					Count:        data.Count,
					Hop:          cur.depth + 1,
					ConnectedVia: sorted(shared),
				})
				if cur.depth+1 < maxHops {
					queue = append(queue, frontier{room, cur.depth + 1})
				}
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Hop != results[j].Hop {
			return results[i].Hop < results[j].Hop
		}
		return results[i].Count > results[j].Count
	})
	if len(results) > 50 {
		results = results[:50]
	}
	return results, nil
}

// FindTunnels returns rooms that span multiple wings.
func FindTunnels(p *palace.Palace, wingA, wingB string) ([]TunnelResult, error) {
	nodes, _, err := BuildGraph(p)
	if err != nil {
		return nil, err
	}

	var tunnels []TunnelResult
	for _, node := range nodes {
		if len(node.Wings) < 2 {
			continue
		}
		if wingA != "" && !contains(node.Wings, wingA) {
			continue
		}
		if wingB != "" && !contains(node.Wings, wingB) {
			continue
		}
		recent := ""
		if len(node.Dates) > 0 {
			recent = node.Dates[len(node.Dates)-1]
		}
		tunnels = append(tunnels, TunnelResult{
			Room:   node.Room,
			Wings:  node.Wings,
			Halls:  node.Halls,
			Count:  node.Count,
			Recent: recent,
		})
	}

	sort.Slice(tunnels, func(i, j int) bool {
		return tunnels[i].Count > tunnels[j].Count
	})
	if len(tunnels) > 50 {
		tunnels = tunnels[:50]
	}
	return tunnels, nil
}

// Stats computes summary statistics about the palace graph.
func Stats(p *palace.Palace) (*GraphStats, error) {
	nodes, edges, err := BuildGraph(p)
	if err != nil {
		return nil, err
	}

	tunnelRooms := 0
	wingCounts := map[string]int{}
	for _, n := range nodes {
		if len(n.Wings) >= 2 {
			tunnelRooms++
		}
		for _, w := range n.Wings {
			wingCounts[w]++
		}
	}

	// Top tunnels: rooms sorted by number of wings desc, cap 10
	type roomWings struct {
		room  string
		node  Node
		nwing int
	}
	var rw []roomWings
	for room, n := range nodes {
		if len(n.Wings) >= 2 {
			rw = append(rw, roomWings{room, n, len(n.Wings)})
		}
	}
	sort.Slice(rw, func(i, j int) bool { return rw[i].nwing > rw[j].nwing })
	if len(rw) > 10 {
		rw = rw[:10]
	}
	topTunnels := make([]TunnelResult, len(rw))
	for i, r := range rw {
		topTunnels[i] = TunnelResult{Room: r.room, Wings: r.node.Wings, Count: r.node.Count}
	}

	return &GraphStats{
		TotalRooms:   len(nodes),
		TunnelRooms:  tunnelRooms,
		TotalEdges:   len(edges),
		RoomsPerWing: wingCounts,
		TopTunnels:   topTunnels,
	}, nil
}

func fuzzyMatch(query string, nodes map[string]Node, n int) []string {
	queryLower := strings.ToLower(query)
	type scored struct {
		room  string
		score float64
	}
	var matches []scored
	for room := range nodes {
		if strings.Contains(room, queryLower) {
			matches = append(matches, scored{room, 1.0})
		} else {
			for _, word := range strings.Split(queryLower, "-") {
				if word != "" && strings.Contains(room, word) {
					matches = append(matches, scored{room, 0.5})
					break
				}
			}
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	if len(matches) > n {
		matches = matches[:n]
	}
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.room
	}
	return out
}

// helpers

type roomAgg struct {
	wings map[string]bool
	halls map[string]bool
	dates map[string]bool
	count int
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func intersect(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		if b[k] {
			out[k] = true
		}
	}
	return out
}

func sorted(m map[string]bool) []string {
	return sortedKeys(m)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
