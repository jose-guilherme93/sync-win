package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	"sync-win/agent/collectors"
	"sync-win/agent/internal/contract"
)

func syncWorkspaceConfigs(serverURL, deviceID, deviceToken string, st *agentState) error {
	if st.LastWorkspaceHashes == nil {
		st.LastWorkspaceHashes = map[string]string{}
	}
	status, body, err := getJSON(serverURL+"/api/devices/"+deviceID+"/sync-config", deviceToken)
	if err != nil {
		return fmt.Errorf("read workspace sync config: %w", err)
	}
	if status >= 400 {
		return fmt.Errorf("workspace sync config returned %d", status)
	}
	var config saveConfigResponse
	if err := json.Unmarshal(body, &config); err != nil {
		return fmt.Errorf("decode workspace sync config: %w", err)
	}

	roots := make([]string, 0, len(config.WorkspaceDirs))
	for _, root := range config.WorkspaceDirs {
		roots = append(roots, contract.ExpandPath(root))
	}
	candidates, err := collectors.DiscoverWorkspaceFiles(agentHome(), collectors.WorkspaceDiscoveryConfig{
		Roots:         roots,
		Files:         syncwinContract.Collection.Workspace.Files,
		MaxFileBytes:  syncwinContract.Collection.Workspace.MaxFileBytes,
		MaxTotalBytes: syncwinContract.Collection.Workspace.MaxTotalBytes,
	})
	if err != nil {
		return fmt.Errorf("discover workspace files: %w", err)
	}
	payloads, hashes := scanWorkspaceCandidates(candidates, st)
	if len(payloads) == 0 {
		pruneWorkspaceHashes(st, hashes)
		st.LastWorkspaceSync = time.Now().UTC()
		st.save()
		return nil
	}
	chunks, err := chunkSavePayloads(payloads, deviceToken)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		responseBody, err := postJSONWithResponse(serverURL+"/api/devices/"+deviceID+"/sync", deviceToken, map[string]any{
			"device_token": deviceToken,
			"preferences":  chunk,
		})
		if err != nil {
			return err
		}
		var response preferenceSyncResponse
		if err := json.Unmarshal(responseBody, &response); err != nil {
			return fmt.Errorf("decode workspace sync response: %w", err)
		}
		for _, saved := range response.Saved {
			if hash, ok := hashes[saved.RelativePath]; ok {
				st.LastWorkspaceHashes[saved.RelativePath] = hash
			}
		}
		st.save()
		log.Printf("workspace sync chunk completed candidates=%d saved=%d rejected=%d", len(chunk), len(response.Saved), len(response.Rejected))
	}
	pruneWorkspaceHashes(st, hashes)
	st.LastWorkspaceSync = time.Now().UTC()
	st.save()
	return nil
}

func scanWorkspaceCandidates(candidates []collectors.SaveCandidate, st *agentState) ([]preferencePayload, map[string]string) {
	hashes := make(map[string]string, len(candidates))
	payloads := make([]preferencePayload, 0, len(candidates))
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate.Path)
		if err != nil || len(data) == 0 || int64(len(data)) > syncwinContract.Collection.Workspace.MaxFileBytes {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) || findSecret(string(data)) != "" {
			continue
		}
		hash := hashContent(string(data))
		hashes[candidate.RelativePath] = hash
		if st.LastWorkspaceHashes[candidate.RelativePath] == hash {
			continue
		}
		payloads = append(payloads, preferencePayload{
			Category:     "workspace",
			Filename:     filepath.Base(candidate.Path),
			RelativePath: candidate.RelativePath,
			Content:      string(data),
		})
	}
	return payloads, hashes
}

func pruneWorkspaceHashes(st *agentState, keep map[string]string) {
	for path := range st.LastWorkspaceHashes {
		if _, ok := keep[path]; !ok {
			delete(st.LastWorkspaceHashes, path)
		}
	}
}
