package haproxy

import (
	"testing"
)

const sampleCSV = `# pxname,svname,qcur,qmax,scur,smax,slim,stot,bin,bout,dreq,dresp,ereq,econ,eresp,wretr,wredis,status,weight,act,bck,chkfail,chkdown,lastchg,downtime,qlimit,pid,iid,sid,throttle,lbtot,tracked,type,rate,rate_lim,rate_max,check_status,check_code,check_duration,hrsp_1xx,hrsp_2xx,hrsp_3xx,hrsp_4xx,hrsp_5xx,hrsp_other,hanafail,req_rate,req_rate_max,req_tot,cli_abrt,srv_abrt,comp_in,comp_out,comp_byp,comp_rsp,lastsess,last_chk,last_agt,qtime,ctime,rtime,ttime,
https_front,FRONTEND,,,5,100,20000,123456,987654321,123456789,10,5,2,0,,,,OPEN,,,,,,,,,1,2,0,,,,0,50,,500,,,,,12345,678,90,12,0,,100,500,123456,,,,,,,,,,,
myapp_backend,myapp_srv,0,5,2,50,,5000,123456,654321,,,,,0,0,0,UP,100,1,0,0,0,86400,0,,1,3,1,,1000,,2,10,,100,L7OK,200,5,0,4500,400,80,20,0,,,,0,0,,,,,10,OK,,5,10,50,100,
myapp_backend,BACKEND,0,5,2,50,,5000,123456,654321,0,0,,0,0,0,0,UP,100,1,0,,0,86400,0,,1,3,0,,1000,,1,10,,100,,,,,4500,400,80,20,0,,,,0,0,,,,,10,,,5,10,50,100,
stats,FRONTEND,,,0,10,10000,1000,50000,25000,0,0,0,0,,,,OPEN,,,,,,,,,1,4,0,,,,0,5,,50,,,,,900,50,40,10,0,,10,100,1000,,,,,,,,,,,
`

func TestParseStatsCSV(t *testing.T) {
	stats, err := ParseStatsCSV(sampleCSV)
	if err != nil {
		t.Fatalf("ParseStatsCSV error: %v", err)
	}

	// Check frontends
	if len(stats.Frontends) != 2 {
		t.Errorf("Expected 2 frontends, got %d", len(stats.Frontends))
	}

	// Check backends
	if len(stats.Backends) != 1 {
		t.Errorf("Expected 1 backend, got %d", len(stats.Backends))
	}

	// Verify frontend data
	var httpsFront *Frontend
	for i := range stats.Frontends {
		if stats.Frontends[i].Name == "https_front" {
			httpsFront = &stats.Frontends[i]
			break
		}
	}

	if httpsFront == nil {
		t.Fatal("https_front not found")
	}

	if httpsFront.Status != "OPEN" {
		t.Errorf("https_front status = %s, want OPEN", httpsFront.Status)
	}
	if httpsFront.CurrentSessions != 5 {
		t.Errorf("https_front scur = %d, want 5", httpsFront.CurrentSessions)
	}
	if httpsFront.TotalSessions != 123456 {
		t.Errorf("https_front stot = %d, want 123456", httpsFront.TotalSessions)
	}
	if httpsFront.Response2xx != 12345 {
		t.Errorf("https_front hrsp_2xx = %d, want 12345", httpsFront.Response2xx)
	}

	// Verify backend data
	backend := stats.Backends[0]
	if backend.Name != "myapp_backend" {
		t.Errorf("backend name = %s, want myapp_backend", backend.Name)
	}
	if backend.Status != "UP" {
		t.Errorf("backend status = %s, want UP", backend.Status)
	}

	// Verify backend has server
	if len(backend.Servers) != 1 {
		t.Errorf("backend servers count = %d, want 1", len(backend.Servers))
	}

	if len(backend.Servers) > 0 {
		server := backend.Servers[0]
		if server.Name != "myapp_srv" {
			t.Errorf("server name = %s, want myapp_srv", server.Name)
		}
		if server.Status != "UP" {
			t.Errorf("server status = %s, want UP", server.Status)
		}
		if server.HealthPercent != 100.0 {
			t.Errorf("server health_percent = %f, want 100.0", server.HealthPercent)
		}
	}

	// Verify calculated fields
	if backend.ActiveServers != 1 {
		t.Errorf("backend active_servers = %d, want 1", backend.ActiveServers)
	}
	if backend.HealthPercent != 100.0 {
		t.Errorf("backend health_percent = %f, want 100.0", backend.HealthPercent)
	}
}

func TestParseStatsCSV_EmptyData(t *testing.T) {
	_, err := ParseStatsCSV("")
	if err == nil {
		t.Error("Expected error for empty CSV data")
	}
}

func TestParseStatsCSV_NoDataRows(t *testing.T) {
	csv := "# pxname,svname,qcur\n"
	_, err := ParseStatsCSV(csv)
	if err == nil {
		t.Error("Expected error for CSV with no data rows")
	}
}

func TestParseStatsCSV_DownServer(t *testing.T) {
	csv := `# pxname,svname,status,scur,smax,stot
test_backend,test_srv,DOWN,0,0,0
test_backend,BACKEND,UP,0,0,0
`
	stats, err := ParseStatsCSV(csv)
	if err != nil {
		t.Fatalf("ParseStatsCSV error: %v", err)
	}

	if len(stats.Backends) != 1 {
		t.Fatalf("Expected 1 backend, got %d", len(stats.Backends))
	}

	backend := stats.Backends[0]
	if backend.DownServers != 1 {
		t.Errorf("down_servers = %d, want 1", backend.DownServers)
	}
	if backend.HealthPercent != 0.0 {
		t.Errorf("health_percent = %f, want 0.0", backend.HealthPercent)
	}

	if len(backend.Servers) > 0 {
		server := backend.Servers[0]
		if server.HealthPercent != 0.0 {
			t.Errorf("server health_percent = %f, want 0.0", server.HealthPercent)
		}
	}
}

func TestMakeColumnMap(t *testing.T) {
	header := []string{"pxname", "svname", "scur", "smax"}
	colMap := makeColumnMap(header)

	if colMap["pxname"] != 0 {
		t.Errorf("pxname index = %d, want 0", colMap["pxname"])
	}
	if colMap["smax"] != 3 {
		t.Errorf("smax index = %d, want 3", colMap["smax"])
	}
}

func TestGetField(t *testing.T) {
	record := []string{"value1", "value2", "value3"}
	colMap := map[string]int{"a": 0, "b": 1, "c": 2}

	if getField(record, colMap, "a") != "value1" {
		t.Error("getField(a) failed")
	}
	if getField(record, colMap, "c") != "value3" {
		t.Error("getField(c) failed")
	}
	if getField(record, colMap, "nonexistent") != "" {
		t.Error("getField(nonexistent) should return empty string")
	}
}

func TestGetInt64Field(t *testing.T) {
	record := []string{"123", "456", "invalid", ""}
	colMap := map[string]int{"a": 0, "b": 1, "c": 2, "d": 3}

	if getInt64Field(record, colMap, "a") != 123 {
		t.Error("getInt64Field(a) failed")
	}
	if getInt64Field(record, colMap, "c") != 0 {
		t.Error("getInt64Field(invalid) should return 0")
	}
	if getInt64Field(record, colMap, "d") != 0 {
		t.Error("getInt64Field(empty) should return 0")
	}
}
