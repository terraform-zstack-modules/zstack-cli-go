package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const (
	vmTestUUID     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rootTestUUID   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	groupTestUUID  = "cccccccccccccccccccccccccccccccc"
	targetTestUUID = "dddddddddddddddddddddddddddddddd"
)

func TestVMOperationsDryRunIsOffline(t *testing.T) {
	var requests int
	cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "unexpected network request", 500)
	})
	cases := [][]string{
		{"instance", "migrate", "vm", "--host", "host"},
		{"instance", "migrate-storage", "vm", "--primary-storage", "storage", "--with-data-volumes"},
		{"instance", "clone", "vm", "--name", "clone", "--full"},
		{"instance", "backup", "vm", "--backup-storage", "storage", "--name", "backup"},
		{"volume", "backup", "disk", "--backup-storage", "storage", "--name", "backup"},
		{"snapshot", "create-group", "vm", "--name", "checkpoint"},
		{"snapshot", "revert-group", groupTestUUID},
		{"instance", "revert-backup", groupTestUUID, "--instance", vmTestUUID},
		{"instance", "create-from-backup", groupTestUUID, "--name", "restored", "--cpu", "1", "--memory", "1G"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args[:2], "/"), func(t *testing.T) {
			out, stderr, err := invokeCLI(t, cfg, "", append(args, "--dry-run", "-o", "json")...)
			if err != nil {
				t.Fatalf("dry-run failed: %v stdout=%s stderr=%s", err, out, stderr)
			}
			var preview map[string]any
			if err := json.Unmarshal([]byte(out), &preview); err != nil || preview["operation"] == nil {
				t.Fatalf("invalid preview: %v %s", err, out)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("dry-run made %d network requests", requests)
	}
}

func TestVMOperationsRejectMissingTargetBeforeNetwork(t *testing.T) {
	var requests int
	cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) { requests++; http.Error(w, "unexpected", 500) })
	for _, args := range [][]string{
		{"instance", "migrate", "vm"},
		{"instance", "migrate-storage", "vm"},
		{"instance", "backup", "vm", "--name", "backup"},
		{"snapshot", "create-group", "vm"},
		{"instance", "revert-backup", "bad-group"},
		{"instance", "revert-backup", groupTestUUID},
		{"instance", "create-from-backup", groupTestUUID, "--name", "restored"},
	} {
		if _, _, err := invokeCLI(t, cfg, "", args...); err == nil {
			t.Fatalf("accepted invalid args: %v", args)
		}
	}
	if requests != 0 {
		t.Fatalf("invalid commands made %d network requests", requests)
	}
}

func TestVMOperationsRequestContracts(t *testing.T) {
	type observed struct {
		Method, Path string
		Body         map[string]any
	}
	var mu sync.Mutex
	var writes []observed
	cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/zstack/v1/vm-instances":
				inventory(w, map[string]any{"uuid": vmTestUUID, "name": "vm", "state": "Running"})
			case "/zstack/v1/vm-instances/" + vmTestUUID:
				fmt.Fprintf(w, `{"inventories":[{"uuid":"%s","rootVolumeUuid":"%s"}]}`, vmTestUUID, rootTestUUID)
			case "/zstack/v1/hosts":
				inventory(w, map[string]any{"uuid": targetTestUUID, "name": "host"})
			case "/zstack/v1/primary-storage":
				inventory(w, map[string]any{"uuid": targetTestUUID, "name": "storage"})
			case "/zstack/v1/backup-storage":
				inventory(w, map[string]any{"uuid": targetTestUUID, "name": "storage"})
			case "/zstack/v1/volumes":
				inventory(w, map[string]any{"uuid": rootTestUUID, "name": "disk"})
			case "/zstack/v1/volume-backups":
				inventory(w, map[string]any{"uuid": rootTestUUID, "name": "backup", "groupUuid": groupTestUUID, "vmInstanceUuid": vmTestUUID})
			case "/zstack/v1/volume-snapshots/group/" + groupTestUUID:
				fmt.Fprintf(w, `{"inventories":[{"uuid":"%s","vmInstanceUuid":"%s"}]}`, groupTestUUID, vmTestUUID)
			default:
				t.Errorf("unexpected GET %s", r.URL.Path)
				fmt.Fprint(w, `{}`)
			}
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		writes = append(writes, observed{r.Method, r.URL.Path, body})
		mu.Unlock()
		switch {
		case body["migrateVm"] != nil || body["primaryStorageMigrateVm"] != nil:
			fmt.Fprintf(w, `{"inventory":{"uuid":"%s","name":"result"}}`, vmTestUUID)
		case body["cloneVmInstance"] != nil:
			fmt.Fprintf(w, `{"success":true,"result":{"numberOfClonedVm":1,"inventories":[{"inventory":{"uuid":"%s"}}]}}`, targetTestUUID)
		case strings.HasSuffix(r.URL.Path, "/vm-backups"):
			fmt.Fprintf(w, `{"inventories":[{"uuid":"%s","groupUuid":"%s"}]}`, rootTestUUID, groupTestUUID)
		case strings.HasSuffix(r.URL.Path, "/volume-backups"):
			fmt.Fprintf(w, `{"inventory":{"uuid":"%s"}}`, rootTestUUID)
		case body["revertVmFromVmBackup"] != nil || body["revertVmFromSnapshotGroup"] != nil:
			fmt.Fprint(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/volume-snapshots/group"):
			fmt.Fprintf(w, `{"inventory":{"uuid":"%s"}}`, groupTestUUID)
		default:
			fmt.Fprintf(w, `{"inventory":{"uuid":"%s","name":"result"}}`, targetTestUUID)
		}
	})
	cases := []struct {
		args              []string
		method, path, key string
	}{
		{[]string{"instance", "migrate", "vm", "--host", "host"}, "PUT", "/zstack/v1/vm-instances/" + vmTestUUID + "/actions", "migrateVm"},
		{[]string{"instance", "migrate-storage", "vm", "--primary-storage", "storage", "--with-data-volumes"}, "PUT", "/zstack/v1/vm-instances/" + vmTestUUID + "/actions", "primaryStorageMigrateVm"},
		{[]string{"instance", "clone", "vm", "--name", "clone", "--full"}, "PUT", "/zstack/v1/vm-instances/" + vmTestUUID + "/actions", "cloneVmInstance"},
		{[]string{"instance", "backup", "vm", "--backup-storage", "storage", "--name", "backup"}, "POST", "/zstack/v1/volumes/" + rootTestUUID + "/vm-backups", "params"},
		{[]string{"volume", "backup", "disk", "--backup-storage", "storage", "--name", "backup"}, "POST", "/zstack/v1/volumes/" + rootTestUUID + "/volume-backups", "params"},
		{[]string{"snapshot", "create-group", "vm", "--name", "checkpoint"}, "POST", "/zstack/v1/volume-snapshots/group", "params"},
		{[]string{"snapshot", "revert-group", groupTestUUID, "--yes"}, "PUT", "/zstack/v1/volume-snapshots/group/" + groupTestUUID + "/actions", "revertVmFromSnapshotGroup"},
		{[]string{"instance", "revert-backup", groupTestUUID, "--instance", vmTestUUID, "--yes"}, "PUT", "/zstack/v1/vm-backups/" + groupTestUUID + "/actions", "revertVmFromVmBackup"},
		{[]string{"instance", "create-from-backup", groupTestUUID, "--name", "restored", "--cpu", "1", "--memory", "1G"}, "POST", "/zstack/v1/vm-instances/from/vm-backups/" + groupTestUUID, "params"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args[:2], "/"), func(t *testing.T) {
			mu.Lock()
			writes = nil
			mu.Unlock()
			out, stderr, err := invokeCLI(t, cfg, "", tc.args...)
			if err != nil {
				t.Fatalf("command failed: %v stdout=%s stderr=%s", err, out, stderr)
			}
			mu.Lock()
			got := append([]observed(nil), writes...)
			mu.Unlock()
			if len(got) != 1 || got[0].Method != tc.method || got[0].Path != tc.path || got[0].Body[tc.key] == nil {
				t.Fatalf("unexpected writes: %+v", got)
			}
			body, ok := got[0].Body[tc.key].(map[string]any)
			if !ok {
				t.Fatalf("missing typed request body: %+v", got[0].Body)
			}
			switch tc.key {
			case "migrateVm":
				if body["hostUuid"] != targetTestUUID {
					t.Fatalf("unresolved host: %+v", body)
				}
			case "primaryStorageMigrateVm":
				if body["dstPrimaryStorageUuid"] != targetTestUUID || body["withDataVolumes"] != true {
					t.Fatalf("storage flags lost: %+v", body)
				}
			case "cloneVmInstance":
				if body["full"] != true || fmt.Sprint(body["names"]) != "[clone]" {
					t.Fatalf("clone scope lost: %+v", body)
				}
			case "params":
				if strings.Contains(tc.path, "/from/vm-backups/") && (body["cpuNum"] != float64(1) || body["memorySize"] != float64(1073741824)) {
					t.Fatalf("custom VM size lost: %+v", body)
				}
				if strings.HasSuffix(tc.path, "/vm-backups") || strings.HasSuffix(tc.path, "/volume-backups") {
					if body["backupStorageUuid"] != targetTestUUID {
						t.Fatalf("unresolved backup storage: %+v", body)
					}
				}
				if strings.HasSuffix(tc.path, "/volume-snapshots/group") && body["rootVolumeUuid"] != rootTestUUID {
					t.Fatalf("snapshot root missing: %+v", body)
				}
			}
		})
	}
}

func TestVMRestoreDeclinedBeforeWrite(t *testing.T) {
	var writes int
	cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
			fmt.Fprint(w, `{}`)
			return
		}
		if strings.Contains(r.URL.Path, "volume-backups") {
			inventory(w, map[string]any{"uuid": rootTestUUID, "groupUuid": groupTestUUID, "vmInstanceUuid": vmTestUUID})
			return
		}
		if strings.Contains(r.URL.Path, "vm-instances") {
			inventory(w, map[string]any{"uuid": vmTestUUID, "name": "original"})
			return
		}
		fmt.Fprintf(w, `{"inventories":[{"uuid":"%s","vmInstanceUuid":"%s"}]}`, groupTestUUID, vmTestUUID)
	})
	for _, args := range [][]string{{"instance", "revert-backup", groupTestUUID, "--instance", vmTestUUID}, {"snapshot", "revert-group", groupTestUUID}} {
		out, stderr, err := invokeCLI(t, cfg, "no\n", args...)
		if err != nil || !strings.Contains(out, "Aborted") {
			t.Fatalf("decline failed: %v %s %s", err, out, stderr)
		}
		out, stderr, err = invokeCLI(t, cfg, "no\n", append(args, "-o", "json")...)
		if err != nil || out != "" || !strings.Contains(stderr, "Aborted") {
			t.Fatalf("JSON decline contaminated stdout: %v %q %q", err, out, stderr)
		}
	}
	if writes != 0 {
		t.Fatalf("declined restore sent %d writes", writes)
	}
}

func TestVMBackupRestoreRejectsWrongOrMissingSourceVM(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owners []string
	}{
		{"wrong VM", []string{targetTestUUID}},
		{"mixed VMs", []string{vmTestUUID, targetTestUUID}},
		{"missing VM identity", []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			writes := 0
			cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet {
					mu.Lock()
					writes++
					mu.Unlock()
					fmt.Fprint(w, `{}`)
					return
				}
				if r.URL.Path != "/zstack/v1/volume-backups" {
					t.Errorf("unexpected lookup %s", r.URL.Path)
				}
				rows := make([]map[string]any, 0, len(tc.owners))
				for _, owner := range tc.owners {
					rows = append(rows, map[string]any{"uuid": rootTestUUID, "groupUuid": groupTestUUID, "vmInstanceUuid": owner})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"inventories": rows})
			})
			out, stderr, err := invokeCLI(t, cfg, "", "instance", "revert-backup", groupTestUUID, "--instance", vmTestUUID, "--yes", "-o", "json")
			mu.Lock()
			gotWrites := writes
			mu.Unlock()
			if err == nil || gotWrites != 0 || out != "" || !strings.Contains(stderr, "identity") {
				t.Fatalf("unsafe restore accepted: %v writes=%d stdout=%s stderr=%s", err, gotWrites, out, stderr)
			}
		})
	}
}

func TestVMOperationsRejectUnverifiableMutationResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		uuid string
	}{
		{"host migration wrong VM", []string{"instance", "migrate", "vm", "--host", "host"}, targetTestUUID},
		{"host migration no VM", []string{"instance", "migrate", "vm", "--host", "host"}, ""},
		{"storage migration wrong VM", []string{"instance", "migrate-storage", "vm", "--primary-storage", "storage"}, targetTestUUID},
		{"storage migration no VM", []string{"instance", "migrate-storage", "vm", "--primary-storage", "storage"}, ""},
		{"snapshot group no UUID", []string{"snapshot", "create-group", "vm", "--name", "checkpoint"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			writes := 0
			cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					switch r.URL.Path {
					case "/zstack/v1/vm-instances":
						inventory(w, map[string]any{"uuid": vmTestUUID, "name": "vm", "state": "Running"})
					case "/zstack/v1/vm-instances/" + vmTestUUID:
						fmt.Fprintf(w, `{"inventory":{"uuid":"%s","rootVolumeUuid":"%s"}}`, vmTestUUID, rootTestUUID)
					case "/zstack/v1/hosts":
						inventory(w, map[string]any{"uuid": targetTestUUID, "name": "host"})
					case "/zstack/v1/primary-storage":
						inventory(w, map[string]any{"uuid": targetTestUUID, "name": "storage"})
					default:
						t.Errorf("unexpected lookup %s", r.URL.Path)
					}
					return
				}
				mu.Lock()
				writes++
				mu.Unlock()
				fmt.Fprintf(w, `{"inventory":{"uuid":"%s"}}`, tc.uuid)
			})
			out, stderr, err := invokeCLI(t, cfg, "", append(tc.args, "-o", "json")...)
			mu.Lock()
			gotWrites := writes
			mu.Unlock()
			if err == nil || gotWrites != 1 || out != "" || !strings.Contains(stderr, "query") {
				t.Fatalf("unverified result accepted: %v writes=%d stdout=%s stderr=%s", err, gotWrites, out, stderr)
			}
		})
	}
}

func TestVMBackupAndSnapshotGroupQueries(t *testing.T) {
	cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected write: %s", r.Method)
		}
		switch r.URL.Path {
		case "/zstack/v1/volume-backups":
			inventory(w, map[string]any{"uuid": rootTestUUID, "groupUuid": groupTestUUID, "name": "backup"})
		case "/zstack/v1/volume-snapshots/group":
			inventory(w, map[string]any{"uuid": groupTestUUID, "vmInstanceUuid": vmTestUUID, "name": "checkpoint", "snapshotCount": 2})
		default:
			t.Errorf("unexpected query: %s", r.URL.Path)
		}
	})
	for _, tc := range []struct{ resource, field, expected string }{{"volume-backups", "groupUuid", groupTestUUID}, {"snapshot-groups", "snapshotCount", "2"}} {
		out, stderr, err := invokeCLI(t, cfg, "", "get", tc.resource, "-o", "json")
		if err != nil {
			t.Fatalf("get %s: %v %s", tc.resource, err, stderr)
		}
		var rows []map[string]any
		if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || fmt.Sprint(rows[0][tc.field]) != tc.expected {
			t.Fatalf("get %s: %v %s", tc.resource, err, out)
		}
	}
}

func TestVMOperationCleanupCommandsCheckIdentityAndDelete(t *testing.T) {
	var mu sync.Mutex
	var writes []string
	cfg := mockConfig(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/zstack/v1/volume-backups":
				inventory(w, map[string]any{"uuid": rootTestUUID, "groupUuid": groupTestUUID})
			case "/zstack/v1/volume-backups/" + rootTestUUID:
				fmt.Fprintf(w, `{"inventories":[{"uuid":"%s"}]}`, rootTestUUID)
			case "/zstack/v1/volume-snapshots/group/" + groupTestUUID:
				fmt.Fprintf(w, `{"inventories":[{"uuid":"%s"}]}`, groupTestUUID)
			default:
				t.Errorf("unexpected cleanup lookup %s", r.URL.Path)
			}
			return
		}
		mu.Lock()
		writes = append(writes, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		fmt.Fprint(w, `{}`)
	})
	cases := []struct{ kind, uuid, path string }{
		{"vm-backup", groupTestUUID, "/zstack/v1/vm-backups/" + groupTestUUID},
		{"volume-backup", rootTestUUID, "/zstack/v1/volume-backups/" + rootTestUUID},
		{"snapshot-group", groupTestUUID, "/zstack/v1/volume-snapshots/group/" + groupTestUUID},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			mu.Lock()
			writes = nil
			mu.Unlock()
			out, stderr, err := invokeCLI(t, cfg, "", "delete", tc.kind, tc.uuid, "--context", "mock", "--dry-run", "-o", "json")
			if err != nil || !strings.Contains(out, tc.uuid) {
				t.Fatalf("dry-run: %v %q %q", err, out, stderr)
			}
			mu.Lock()
			count := len(writes)
			mu.Unlock()
			if count != 0 {
				t.Fatalf("dry-run sent writes: %d", count)
			}
			out, stderr, err = invokeCLI(t, cfg, "", "delete", tc.kind, tc.uuid, "--context", "mock", "--yes", "-o", "json")
			if err != nil {
				t.Fatalf("delete: %v %q %q", err, out, stderr)
			}
			mu.Lock()
			got := append([]string(nil), writes...)
			mu.Unlock()
			if len(got) != 1 || !strings.HasPrefix(got[0], "DELETE "+tc.path+"?") || !strings.Contains(got[0], "deleteMode=Permissive") {
				t.Fatalf("unexpected deletes: %v", got)
			}
		})
	}
}
