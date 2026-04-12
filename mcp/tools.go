package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"go-palace/internal/graph"
	"go-palace/pkg/kg"
	"go-palace/pkg/palace"
	"go-palace/pkg/searcher"
)

// toolHandler is the signature for all tool dispatch functions.
type toolHandler func(s *Server, args map[string]any) map[string]any

// toolRegistry maps tool names to their handler + input schema.
var toolRegistry map[string]struct {
	handler toolHandler
	schema  map[string]any
}

func init() {
	toolRegistry = map[string]struct {
		handler toolHandler
		schema  map[string]any
	}{
		"mempalace_status":          {handler: toolStatus, schema: toolDefs["mempalace_status"]},
		"mempalace_list_wings":      {handler: toolListWings, schema: toolDefs["mempalace_list_wings"]},
		"mempalace_list_rooms":      {handler: toolListRooms, schema: toolDefs["mempalace_list_rooms"]},
		"mempalace_get_taxonomy":    {handler: toolGetTaxonomy, schema: toolDefs["mempalace_get_taxonomy"]},
		"mempalace_get_aaak_spec":   {handler: toolGetAAAKSpec, schema: toolDefs["mempalace_get_aaak_spec"]},
		"mempalace_search":          {handler: toolSearch, schema: toolDefs["mempalace_search"]},
		"mempalace_check_duplicate": {handler: toolCheckDuplicate, schema: toolDefs["mempalace_check_duplicate"]},
		"mempalace_add_drawer":      {handler: toolAddDrawer, schema: toolDefs["mempalace_add_drawer"]},
		"mempalace_delete_drawer":   {handler: toolDeleteDrawer, schema: toolDefs["mempalace_delete_drawer"]},
		"mempalace_kg_query":        {handler: toolKGQuery, schema: toolDefs["mempalace_kg_query"]},
		"mempalace_kg_add":          {handler: toolKGAdd, schema: toolDefs["mempalace_kg_add"]},
		"mempalace_kg_invalidate":   {handler: toolKGInvalidate, schema: toolDefs["mempalace_kg_invalidate"]},
		"mempalace_kg_timeline":     {handler: toolKGTimeline, schema: toolDefs["mempalace_kg_timeline"]},
		"mempalace_kg_stats":        {handler: toolKGStats, schema: toolDefs["mempalace_kg_stats"]},
		"mempalace_diary_write":     {handler: toolDiaryWrite, schema: toolDefs["mempalace_diary_write"]},
		"mempalace_diary_read":      {handler: toolDiaryRead, schema: toolDefs["mempalace_diary_read"]},
		"mempalace_traverse":        {handler: toolTraverse, schema: toolDefs["mempalace_traverse"]},
		"mempalace_find_tunnels":    {handler: toolFindTunnels, schema: toolDefs["mempalace_find_tunnels"]},
		"mempalace_graph_stats":     {handler: toolGraphStats, schema: toolDefs["mempalace_graph_stats"]},
	}
}

func lookupTool(name string) (toolHandler, map[string]any) {
	entry, ok := toolRegistry[name]
	if !ok {
		return nil, nil
	}
	return entry.handler, entry.schema
}

// toolDefinitions returns the ordered list of tool definitions for tools/list.
func toolDefinitions() []ToolDef {
	names := toolOrder
	defs := make([]ToolDef, 0, len(names))
	for _, name := range names {
		schema := toolDefs[name]
		desc, _ := schema["_description"].(string)
		inputSchema := map[string]any{
			"type":       "object",
			"properties": schema["properties"],
		}
		if req, ok := schema["required"]; ok {
			inputSchema["required"] = req
		}
		defs = append(defs, ToolDef{
			Name:        name,
			Description: desc,
			InputSchema: inputSchema,
		})
	}
	return defs
}

// --- tool implementations ---

func toolStatus(s *Server, _ map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	count, err := s.palace.Count()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	wings, rooms := aggregateWingsRooms(s.palace)
	return map[string]any{
		"total_drawers": count,
		"wings":         wings,
		"rooms":         rooms,
		"palace_path":   s.palacePath,
		"protocol":      PalaceProtocol,
		"aaak_dialect":  AAKSpec,
	}
}

func toolListWings(s *Server, _ map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	wings, _ := aggregateWingsRooms(s.palace)
	return map[string]any{"wings": wings}
}

func toolListRooms(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	wing, _ := args["wing"].(string)
	_, rooms := aggregateWingsRooms(s.palace)
	if wing != "" {
		// Filter rooms by wing — need a separate aggregation.
		rooms = aggregateRoomsForWing(s.palace, wing)
	}
	return map[string]any{"wing": stringOr(wing, "all"), "rooms": rooms}
}

func toolGetTaxonomy(s *Server, _ map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	taxonomy := map[string]map[string]int{}
	batchSize := 5000
	offset := 0
	for {
		drawers, err := s.palace.Get(palace.GetOptions{Limit: batchSize, Offset: offset})
		if err != nil || len(drawers) == 0 {
			break
		}
		for _, d := range drawers {
			w := stringOr(d.Wing, "unknown")
			r := stringOr(d.Room, "unknown")
			if taxonomy[w] == nil {
				taxonomy[w] = map[string]int{}
			}
			taxonomy[w][r]++
		}
		offset += len(drawers)
		if len(drawers) < batchSize {
			break
		}
	}
	return map[string]any{"taxonomy": taxonomy}
}

func toolGetAAAKSpec(_ *Server, _ map[string]any) map[string]any {
	return map[string]any{"aaak_spec": AAKSpec}
}

func toolSearch(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	query, _ := args["query"].(string)
	limit := intArg(args, "limit", 5)
	wing, _ := args["wing"].(string)
	room, _ := args["room"].(string)

	results, err := searcher.SearchMemories(s.palace, searcher.SearchOptions{
		Query:    query,
		Wing:     wing,
		Room:     room,
		NResults: limit,
	})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	memories := make([]map[string]any, 0, len(results))
	for _, r := range results {
		memories = append(memories, map[string]any{
			"text":        r.Text,
			"wing":        r.Wing,
			"room":        r.Room,
			"source_file": r.SourceFile,
			"similarity":  r.Similarity,
		})
	}
	return map[string]any{
		"query":    query,
		"memories": memories,
		"count":    len(memories),
	}
}

func toolCheckDuplicate(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	content, _ := args["content"].(string)
	threshold := floatArg(args, "threshold", 0.9)

	results, err := s.palace.Query(content, palace.QueryOptions{NResults: 5})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	var duplicates []map[string]any
	for _, r := range results {
		if r.Similarity >= threshold {
			doc := r.Drawer.Document
			if len(doc) > 200 {
				doc = doc[:200] + "..."
			}
			duplicates = append(duplicates, map[string]any{
				"id":         r.Drawer.ID,
				"wing":       stringOr(r.Drawer.Wing, "?"),
				"room":       stringOr(r.Drawer.Room, "?"),
				"similarity": r.Similarity,
				"content":    doc,
			})
		}
	}
	return map[string]any{
		"is_duplicate": len(duplicates) > 0,
		"matches":      duplicates,
	}
}

func toolAddDrawer(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	wing, _ := args["wing"].(string)
	room, _ := args["room"].(string)
	content, _ := args["content"].(string)
	sourceFile, _ := args["source_file"].(string)
	addedBy, _ := args["added_by"].(string)
	if addedBy == "" {
		addedBy = "mcp"
	}

	wing = sanitizeWingRoom(wing)
	room = sanitizeWingRoom(room)
	content = sanitizeContent(content)

	if wing == "" || room == "" || content == "" {
		return map[string]any{"success": false, "error": "wing, room, and content are required"}
	}

	// Content-hash drawer ID (different from miner's file-based ID).
	hashInput := wing + room + content
	if len(hashInput) > len(wing)+len(room)+100 {
		hashInput = wing + room + content[:100]
	}
	sum := sha256.Sum256([]byte(hashInput))
	drawerID := fmt.Sprintf("drawer_%s_%s_%s", wing, room, hex.EncodeToString(sum[:])[:24])

	walLog("add_drawer", map[string]any{
		"drawer_id":       drawerID,
		"wing":            wing,
		"room":            room,
		"added_by":        addedBy,
		"content_length":  len(content),
		"content_preview": truncate(content, 200),
	}, nil)

	// Idempotency: check if drawer already exists.
	existing, err := s.palace.Get(palace.GetOptions{
		Where: map[string]string{"wing": wing, "room": room},
		Limit: 5000,
	})
	if err == nil {
		for _, d := range existing {
			if d.ID == drawerID {
				return map[string]any{"success": true, "reason": "already_exists", "drawer_id": drawerID}
			}
		}
	}

	d := palace.Drawer{
		ID:         drawerID,
		Document:   content,
		Wing:       wing,
		Room:       room,
		SourceFile: sourceFile,
		ChunkIndex: 0,
		AddedBy:    addedBy,
		FiledAt:    time.Now(),
	}
	if err := s.palace.Upsert(d); err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	return map[string]any{"success": true, "drawer_id": drawerID, "wing": wing, "room": room}
}

func toolDeleteDrawer(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	drawerID, _ := args["drawer_id"].(string)

	walLog("delete_drawer", map[string]any{"drawer_id": drawerID}, nil)

	if err := s.palace.Delete(drawerID); err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	return map[string]any{"success": true, "drawer_id": drawerID}
}

func toolKGQuery(s *Server, args map[string]any) map[string]any {
	if s.kg == nil {
		return map[string]any{"error": "knowledge graph not available"}
	}
	entity, _ := args["entity"].(string)
	asOf, _ := args["as_of"].(string)
	direction, _ := args["direction"].(string)
	if direction == "" {
		direction = "both"
	}

	facts, err := s.kg.QueryEntity(entity, kg.QueryOpts{AsOf: asOf, Direction: direction})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	factMaps := make([]map[string]any, 0, len(facts))
	for _, f := range facts {
		factMaps = append(factMaps, map[string]any{
			"direction":  f.Direction,
			"subject":    f.Subject,
			"predicate":  f.Predicate,
			"object":     f.Object,
			"valid_from": f.ValidFrom,
			"valid_to":   f.ValidTo,
			"confidence": f.Confidence,
			"current":    f.Current,
		})
	}
	return map[string]any{
		"entity": entity,
		"as_of":  asOf,
		"facts":  factMaps,
		"count":  len(factMaps),
	}
}

func toolKGAdd(s *Server, args map[string]any) map[string]any {
	if s.kg == nil {
		return map[string]any{"error": "knowledge graph not available"}
	}
	subject, _ := args["subject"].(string)
	predicate, _ := args["predicate"].(string)
	object, _ := args["object"].(string)
	validFrom, _ := args["valid_from"].(string)
	sourceCloset, _ := args["source_closet"].(string)

	subject = validateName(subject)
	predicate = validateName(predicate)
	object = validateName(object)

	walLog("kg_add", map[string]any{
		"subject": subject, "predicate": predicate, "object": object,
		"valid_from": validFrom, "source_closet": sourceCloset,
	}, nil)

	tripleID, err := s.kg.AddTriple(kg.Triple{
		Subject:      subject,
		Predicate:    predicate,
		Object:       object,
		ValidFrom:    validFrom,
		SourceCloset: sourceCloset,
	})
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	return map[string]any{
		"success":   true,
		"triple_id": tripleID,
		"fact":      fmt.Sprintf("%s → %s → %s", subject, predicate, object),
	}
}

func toolKGInvalidate(s *Server, args map[string]any) map[string]any {
	if s.kg == nil {
		return map[string]any{"error": "knowledge graph not available"}
	}
	subject, _ := args["subject"].(string)
	predicate, _ := args["predicate"].(string)
	object, _ := args["object"].(string)
	ended, _ := args["ended"].(string)

	walLog("kg_invalidate", map[string]any{
		"subject": subject, "predicate": predicate, "object": object, "ended": ended,
	}, nil)

	if err := s.kg.Invalidate(subject, predicate, object, ended); err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	endedDisplay := ended
	if endedDisplay == "" {
		endedDisplay = "today"
	}
	return map[string]any{
		"success": true,
		"fact":    fmt.Sprintf("%s → %s → %s", subject, predicate, object),
		"ended":   endedDisplay,
	}
}

func toolKGTimeline(s *Server, args map[string]any) map[string]any {
	if s.kg == nil {
		return map[string]any{"error": "knowledge graph not available"}
	}
	entity, _ := args["entity"].(string)

	facts, err := s.kg.Timeline(entity)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	timeline := make([]map[string]any, 0, len(facts))
	for _, f := range facts {
		timeline = append(timeline, map[string]any{
			"subject":    f.Subject,
			"predicate":  f.Predicate,
			"object":     f.Object,
			"valid_from": f.ValidFrom,
			"valid_to":   f.ValidTo,
			"current":    f.Current,
		})
	}
	entityLabel := entity
	if entityLabel == "" {
		entityLabel = "all"
	}
	return map[string]any{
		"entity":   entityLabel,
		"timeline": timeline,
		"count":    len(timeline),
	}
}

func toolKGStats(s *Server, _ map[string]any) map[string]any {
	if s.kg == nil {
		return map[string]any{"error": "knowledge graph not available"}
	}
	stats, err := s.kg.Stats()
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{
		"entities":           stats.Entities,
		"triples":            stats.Triples,
		"current_facts":      stats.CurrentFacts,
		"expired_facts":      stats.ExpiredFacts,
		"relationship_types": stats.RelationshipTypes,
	}
}

func toolDiaryWrite(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	agentName, _ := args["agent_name"].(string)
	entry, _ := args["entry"].(string)
	topic, _ := args["topic"].(string)
	if topic == "" {
		topic = "general"
	}

	agentName = validateName(agentName)
	entry = sanitizeContent(entry)

	wing := fmt.Sprintf("wing_%s", strings.ToLower(strings.ReplaceAll(agentName, " ", "_")))
	room := "diary"
	now := time.Now()
	entryHash := sha256.Sum256([]byte(truncate(entry, 50)))
	entryID := fmt.Sprintf("diary_%s_%s_%s", wing, now.Format("20060102_150405"), hex.EncodeToString(entryHash[:])[:12])

	walLog("diary_write", map[string]any{
		"agent_name":    agentName,
		"topic":         topic,
		"entry_id":      entryID,
		"entry_preview": truncate(entry, 200),
	}, nil)

	d := palace.Drawer{
		ID:       entryID,
		Document: entry,
		Wing:     wing,
		Room:     room,
		AddedBy:  agentName,
		FiledAt:  now,
		Metadata: map[string]any{
			"hall":  "hall_diary",
			"topic": topic,
			"type":  "diary_entry",
			"agent": agentName,
			"date":  now.Format("2006-01-02"),
		},
	}
	if err := s.palace.Upsert(d); err != nil {
		return map[string]any{"success": false, "error": err.Error()}
	}
	return map[string]any{
		"success":   true,
		"entry_id":  entryID,
		"agent":     agentName,
		"topic":     topic,
		"timestamp": now.Format(time.RFC3339),
	}
}

func toolDiaryRead(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	agentName, _ := args["agent_name"].(string)
	lastN := intArg(args, "last_n", 10)

	wing := fmt.Sprintf("wing_%s", strings.ToLower(strings.ReplaceAll(agentName, " ", "_")))

	drawers, err := s.palace.Get(palace.GetOptions{
		Where: map[string]string{"wing": wing, "room": "diary"},
		Limit: 10000,
	})
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	if len(drawers) == 0 {
		return map[string]any{
			"agent":   agentName,
			"entries": []any{},
			"message": "No diary entries yet.",
		}
	}

	// Sort by filed_at descending.
	sort.Slice(drawers, func(i, j int) bool {
		return drawers[i].FiledAt.After(drawers[j].FiledAt)
	})
	if len(drawers) > lastN {
		drawers = drawers[:lastN]
	}

	entries := make([]map[string]any, 0, len(drawers))
	for _, d := range drawers {
		topic := ""
		date := ""
		if d.Metadata != nil {
			topic, _ = d.Metadata["topic"].(string)
			date, _ = d.Metadata["date"].(string)
		}
		entries = append(entries, map[string]any{
			"date":      date,
			"timestamp": d.FiledAt.Format(time.RFC3339),
			"topic":     topic,
			"content":   d.Document,
		})
	}
	return map[string]any{
		"agent":   agentName,
		"entries": entries,
		"total":   len(drawers),
		"showing": len(entries),
	}
}

func toolTraverse(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	startRoom, _ := args["start_room"].(string)
	maxHops := intArg(args, "max_hops", 2)

	results, err := graph.Traverse(s.palace, startRoom, maxHops)
	if err != nil {
		if te, ok := err.(*graph.TraverseError); ok {
			return map[string]any{
				"error":       te.Message,
				"suggestions": te.Suggestions,
			}
		}
		return map[string]any{"error": err.Error()}
	}

	nodes := make([]map[string]any, 0, len(results))
	for _, r := range results {
		nodes = append(nodes, map[string]any{
			"room":          r.Room,
			"wings":         r.Wings,
			"halls":         r.Halls,
			"count":         r.Count,
			"hop":           r.Hop,
			"connected_via": r.ConnectedVia,
		})
	}
	return map[string]any{"start": startRoom, "nodes": nodes, "count": len(nodes)}
}

func toolFindTunnels(s *Server, args map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	wingA, _ := args["wing_a"].(string)
	wingB, _ := args["wing_b"].(string)

	tunnels, err := graph.FindTunnels(s.palace, wingA, wingB)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}

	result := make([]map[string]any, 0, len(tunnels))
	for _, t := range tunnels {
		result = append(result, map[string]any{
			"room":   t.Room,
			"wings":  t.Wings,
			"halls":  t.Halls,
			"count":  t.Count,
			"recent": t.Recent,
		})
	}
	return map[string]any{"tunnels": result, "count": len(result)}
}

func toolGraphStats(s *Server, _ map[string]any) map[string]any {
	if s.palace == nil {
		return noPalace()
	}
	stats, err := graph.Stats(s.palace)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{
		"total_rooms":    stats.TotalRooms,
		"tunnel_rooms":   stats.TunnelRooms,
		"total_edges":    stats.TotalEdges,
		"rooms_per_wing": stats.RoomsPerWing,
		"top_tunnels":    stats.TopTunnels,
	}
}

// --- helpers ---

func noPalace() map[string]any {
	return map[string]any{
		"error": "No palace found",
		"hint":  "Run: mempalace init <dir> && mempalace mine <dir>",
	}
}

func aggregateWingsRooms(p *palace.Palace) (map[string]int, map[string]int) {
	wings := map[string]int{}
	rooms := map[string]int{}
	batchSize := 5000
	offset := 0
	for {
		drawers, err := p.Get(palace.GetOptions{Limit: batchSize, Offset: offset})
		if err != nil || len(drawers) == 0 {
			break
		}
		for _, d := range drawers {
			w := stringOr(d.Wing, "unknown")
			r := stringOr(d.Room, "unknown")
			wings[w]++
			rooms[r]++
		}
		offset += len(drawers)
		if len(drawers) < batchSize {
			break
		}
	}
	return wings, rooms
}

func aggregateRoomsForWing(p *palace.Palace, wing string) map[string]int {
	rooms := map[string]int{}
	batchSize := 5000
	offset := 0
	for {
		drawers, err := p.Get(palace.GetOptions{
			Where:  map[string]string{"wing": wing},
			Limit:  batchSize,
			Offset: offset,
		})
		if err != nil || len(drawers) == 0 {
			break
		}
		for _, d := range drawers {
			r := stringOr(d.Room, "unknown")
			rooms[r]++
		}
		offset += len(drawers)
		if len(drawers) < batchSize {
			break
		}
	}
	return rooms
}

func stringOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func intArg(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		var x int
		if n, err := fmt.Sscanf(val, "%d", &x); err == nil && n == 1 {
			return x
		}
	}
	return def
}

func floatArg(args map[string]any, key string, def float64) float64 {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	}
	return def
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// sanitizeWingRoom normalizes a wing/room name: lowercase, only [a-z0-9_-].
var reNameClean = regexp.MustCompile(`[^a-z0-9_\-]`)

func sanitizeWingRoom(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	name = reNameClean.ReplaceAllString(name, "")
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

// validateName validates a name for KG entities: reject path traversal and
// null bytes but preserve casing and content (matches Python sanitize_name).
func validateName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "..", "")
	name = strings.ReplaceAll(name, "/", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "\x00", "")
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

// sanitizeContent strips control characters (except newline/tab) and enforces length.
func sanitizeContent(content string) string {
	var b strings.Builder
	for _, r := range content {
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	const maxLen = 100_000
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// toolOrder ensures deterministic tools/list output.
var toolOrder = []string{
	"mempalace_status",
	"mempalace_list_wings",
	"mempalace_list_rooms",
	"mempalace_get_taxonomy",
	"mempalace_get_aaak_spec",
	"mempalace_kg_query",
	"mempalace_kg_add",
	"mempalace_kg_invalidate",
	"mempalace_kg_timeline",
	"mempalace_kg_stats",
	"mempalace_traverse",
	"mempalace_find_tunnels",
	"mempalace_graph_stats",
	"mempalace_search",
	"mempalace_check_duplicate",
	"mempalace_add_drawer",
	"mempalace_delete_drawer",
	"mempalace_diary_write",
	"mempalace_diary_read",
}

// toolDefs maps each tool name to its schema properties (used for coercion and tools/list).
var toolDefs = map[string]map[string]any{
	"mempalace_status": {
		"_description": "Palace overview — total drawers, wing and room counts",
		"properties":   map[string]any{},
	},
	"mempalace_list_wings": {
		"_description": "List all wings with drawer counts",
		"properties":   map[string]any{},
	},
	"mempalace_list_rooms": {
		"_description": "List rooms within a wing (or all rooms if no wing given)",
		"properties": map[string]any{
			"wing": map[string]any{"type": "string", "description": "Wing to list rooms for (optional)"},
		},
	},
	"mempalace_get_taxonomy": {
		"_description": "Full taxonomy: wing → room → drawer count",
		"properties":   map[string]any{},
	},
	"mempalace_get_aaak_spec": {
		"_description": "Get the AAAK dialect specification — the compressed memory format MemPalace uses. Call this if you need to read or write AAAK-compressed memories.",
		"properties":   map[string]any{},
	},
	"mempalace_kg_query": {
		"_description": "Query the knowledge graph for an entity's relationships. Returns typed facts with temporal validity.",
		"properties": map[string]any{
			"entity":    map[string]any{"type": "string", "description": "Entity to query"},
			"as_of":     map[string]any{"type": "string", "description": "Date filter (YYYY-MM-DD, optional)"},
			"direction": map[string]any{"type": "string", "description": "outgoing, incoming, or both (default: both)"},
		},
		"required": []string{"entity"},
	},
	"mempalace_kg_add": {
		"_description": "Add a fact to the knowledge graph. Subject → predicate → object.",
		"properties": map[string]any{
			"subject":       map[string]any{"type": "string", "description": "The entity doing/being something"},
			"predicate":     map[string]any{"type": "string", "description": "The relationship type"},
			"object":        map[string]any{"type": "string", "description": "The entity being connected to"},
			"valid_from":    map[string]any{"type": "string", "description": "When this became true (YYYY-MM-DD, optional)"},
			"source_closet": map[string]any{"type": "string", "description": "Closet ID (optional)"},
		},
		"required": []string{"subject", "predicate", "object"},
	},
	"mempalace_kg_invalidate": {
		"_description": "Mark a fact as no longer true.",
		"properties": map[string]any{
			"subject":   map[string]any{"type": "string", "description": "Entity"},
			"predicate": map[string]any{"type": "string", "description": "Relationship"},
			"object":    map[string]any{"type": "string", "description": "Connected entity"},
			"ended":     map[string]any{"type": "string", "description": "When it stopped being true (YYYY-MM-DD, default: today)"},
		},
		"required": []string{"subject", "predicate", "object"},
	},
	"mempalace_kg_timeline": {
		"_description": "Chronological timeline of facts.",
		"properties": map[string]any{
			"entity": map[string]any{"type": "string", "description": "Entity to get timeline for (optional)"},
		},
	},
	"mempalace_kg_stats": {
		"_description": "Knowledge graph overview: entities, triples, current vs expired facts.",
		"properties":   map[string]any{},
	},
	"mempalace_traverse": {
		"_description": "Walk the palace graph from a room. Shows connected ideas across wings.",
		"properties": map[string]any{
			"start_room": map[string]any{"type": "string", "description": "Room to start from"},
			"max_hops":   map[string]any{"type": "integer", "description": "How many connections to follow (default: 2)"},
		},
		"required": []string{"start_room"},
	},
	"mempalace_find_tunnels": {
		"_description": "Find rooms that bridge two wings.",
		"properties": map[string]any{
			"wing_a": map[string]any{"type": "string", "description": "First wing (optional)"},
			"wing_b": map[string]any{"type": "string", "description": "Second wing (optional)"},
		},
	},
	"mempalace_graph_stats": {
		"_description": "Palace graph overview: total rooms, tunnel connections, edges.",
		"properties":   map[string]any{},
	},
	"mempalace_search": {
		"_description": "Semantic search. Returns verbatim drawer content with similarity scores.",
		"properties": map[string]any{
			"query":   map[string]any{"type": "string", "description": "Search query"},
			"limit":   map[string]any{"type": "integer", "description": "Max results (default 5)"},
			"wing":    map[string]any{"type": "string", "description": "Filter by wing (optional)"},
			"room":    map[string]any{"type": "string", "description": "Filter by room (optional)"},
			"context": map[string]any{"type": "string", "description": "Background context (optional)"},
		},
		"required": []string{"query"},
	},
	"mempalace_check_duplicate": {
		"_description": "Check if content already exists in the palace before filing.",
		"properties": map[string]any{
			"content":   map[string]any{"type": "string", "description": "Content to check"},
			"threshold": map[string]any{"type": "number", "description": "Similarity threshold 0-1 (default 0.9)"},
		},
		"required": []string{"content"},
	},
	"mempalace_add_drawer": {
		"_description": "File verbatim content into the palace.",
		"properties": map[string]any{
			"wing":        map[string]any{"type": "string", "description": "Wing (project name)"},
			"room":        map[string]any{"type": "string", "description": "Room (aspect)"},
			"content":     map[string]any{"type": "string", "description": "Verbatim content to store"},
			"source_file": map[string]any{"type": "string", "description": "Where this came from (optional)"},
			"added_by":    map[string]any{"type": "string", "description": "Who is filing this (default: mcp)"},
		},
		"required": []string{"wing", "room", "content"},
	},
	"mempalace_delete_drawer": {
		"_description": "Delete a drawer by ID.",
		"properties": map[string]any{
			"drawer_id": map[string]any{"type": "string", "description": "ID of the drawer to delete"},
		},
		"required": []string{"drawer_id"},
	},
	"mempalace_diary_write": {
		"_description": "Write to your personal agent diary.",
		"properties": map[string]any{
			"agent_name": map[string]any{"type": "string", "description": "Your name"},
			"entry":      map[string]any{"type": "string", "description": "Your diary entry"},
			"topic":      map[string]any{"type": "string", "description": "Topic tag (optional, default: general)"},
		},
		"required": []string{"agent_name", "entry"},
	},
	"mempalace_diary_read": {
		"_description": "Read your recent diary entries.",
		"properties": map[string]any{
			"agent_name": map[string]any{"type": "string", "description": "Your name"},
			"last_n":     map[string]any{"type": "integer", "description": "Number of recent entries (default: 10)"},
		},
		"required": []string{"agent_name"},
	},
}
