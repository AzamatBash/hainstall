package haproxy

import "testing"

func TestParseStats(t *testing.T) {
	raw := `# pxname,svname,qcur,qmax,scur,smax,slim,stot,bin,bout,dreq,dresp,ereq,econ,eresp,wretr,wredis,status,weight,act,bck,chkfail,chkdown,lastchg,downtime,qlimit,pid,iid,sid,throttle,lbtot,tracked,type,rate,rate_lim,rate_max,check_status,check_code,check_duration,hrsp_1xx,hrsp_2xx,hrsp_3xx,hrsp_4xx,hrsp_5xx,hrsp_other,hanafail,req_rate,req_rate_max,req_tot,cli_abrt,srv_abrt,comp_in,comp_out,comp_byp,comp_rsp,lastsess,last_chk,last_agt,qtime,ctime,rtime,ttime,
https_front,FRONTEND,,,3,10,20000,100,1000,2000,0,0,0,,,,,OPEN,,,,,,,,,,1,1,0,,,,0,1,0,10,,,,0,80,0,5,0,0,,1,5,100,,,0,0,0,0,,,,,,,,
app,srv1,0,0,1,5,,50,100,200,,0,,0,0,0,0,UP,100,1,0,0,0,10,0,,1,2,1,,50,,2,0,,5,L7OK,200,1,0,40,0,5,0,0,0,,,,0,0,,,,,0,,,0,0,0,0,
app,BACKEND,0,0,2,8,20000,50,100,200,0,0,,0,0,0,0,UP,100,1,0,,0,10,0,,1,2,0,,50,,1,0,,5,,,,0,40,0,5,0,0,,,,,0,0,0,0,0,0,0,,,0,0,0,0,
`
	stats := ParseStats(raw)
	if stats.ActiveSessions < 3 {
		t.Fatalf("expected sessions >= 3, got %+v", stats)
	}
	if len(stats.Frontends) != 1 || stats.Frontends[0].Name != "https_front" {
		t.Fatalf("frontends: %+v", stats.Frontends)
	}
	if len(stats.Backends) != 1 || stats.Backends[0].Name != "app" {
		t.Fatalf("backends: %+v", stats.Backends)
	}
	if stats.Backends[0].Servers != 1 {
		t.Fatalf("servers_up: %+v", stats.Backends[0])
	}
	if stats.BytesIn != 1000 || stats.BytesOut != 2000 {
		t.Fatalf("bytes in/out: %+v", stats)
	}
	if stats.Frontends[0].BytesIn != 1000 || stats.Frontends[0].BytesOut != 2000 {
		t.Fatalf("frontend bytes: %+v", stats.Frontends[0])
	}

	servers := ParseServersFromStat(raw)
	if len(servers) != 1 || servers[0].Name != "srv1" || servers[0].Status != "UP" {
		t.Fatalf("servers: %+v", servers)
	}
}

func TestParseServersState(t *testing.T) {
	raw := `# be_id be_name srv_id srv_name srv_addr srv_op_state srv_admin_state srv_uweight srv_iweight srv_time_since_last_change srv_check_status srv_check_result srv_check_health srv_check_state srv_agent_state bk_f_forced_id srv_f_forced_id
1 app 1 srv1 1.2.3.4:443 2 0 100 100 10 6 3 4 6 0 0 0
`
	servers := ParseServersState(raw)
	if len(servers) != 1 {
		t.Fatalf("len=%d %+v", len(servers), servers)
	}
	s := servers[0]
	if s.Backend != "app" || s.Name != "srv1" || s.Address != "1.2.3.4" || s.Port != 443 || s.Weight != 100 {
		t.Fatalf("parse: %+v", s)
	}
}

func TestParseServersStateSeparatePort(t *testing.T) {
	// HAProxy 3.x live format: IP in srv_addr, port in srv_port.
	raw := `1
# be_id be_name srv_id srv_name srv_addr srv_op_state srv_admin_state srv_uweight srv_iweight srv_time_since_last_change srv_check_status srv_check_result srv_check_health srv_check_state srv_agent_state bk_f_forced_id srv_f_forced_id srv_fqdn srv_port srvrecord srv_use_ssl
5 app 1 srv1 1.1.1.1 2 0 100 100 55 6 3 4 6 0 0 0 - 443 - 0
4 acme 1 local 127.0.0.1 2 0 1 1 222 1 0 2 0 0 0 0 - 8080 - 0
`
	servers := ParseServersState(raw)
	if len(servers) != 2 {
		t.Fatalf("len=%d %+v", len(servers), servers)
	}
	byName := map[string]ServerInfo{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	srv1 := byName["srv1"]
	if srv1.Backend != "app" || srv1.Address != "1.1.1.1" || srv1.Port != 443 || srv1.Weight != 100 {
		t.Fatalf("srv1: %+v", srv1)
	}
	local := byName["local"]
	if local.Address != "127.0.0.1" || local.Port != 8080 {
		t.Fatalf("local: %+v", local)
	}
}
