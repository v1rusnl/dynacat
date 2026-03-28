package dynacat

import "testing"

func TestParseBlockyMetrics(t *testing.T) {
	metrics := []byte(`
# HELP blocky_denylist_cache_entries Number of entries in the denylist cache
# TYPE blocky_denylist_cache_entries gauge
blocky_denylist_cache_entries{group="ads"} 139574
blocky_denylist_cache_entries{group="scams"} 14238
# HELP blocky_query_total Number of total queries
# TYPE blocky_query_total counter
blocky_query_total{client="192.168.16.9",type="A"} 38716
blocky_query_total{client="192.168.16.9",type="AAAA"} 13618
blocky_query_total{client="192.168.16.9",type="HTTPS"} 12699
blocky_query_total{client="192.168.16.9",type="PTR"} 6
blocky_query_total{client="192.168.16.9",type="SRV"} 58
blocky_query_total{client="192.168.16.9",type="TXT"} 21
# HELP blocky_response_total Number of total responses
# TYPE blocky_response_total counter
blocky_response_total{reason="BLOCKED (ads)",response_code="NOERROR",response_type="BLOCKED"} 264
blocky_response_total{reason="BLOCKED (ads)",response_code="NXDOMAIN",response_type="BLOCKED"} 65
blocky_response_total{reason="RESOLVED (https://cloudflare-dns.com/dns-query)",response_code="NOERROR",response_type="RESOLVED"} 34306
blocky_response_total{reason="Special-Use Domain Name",response_code="NOERROR",response_type="SPECIAL"} 64
`)

	snapshot, err := parseBlockyMetrics(metrics)
	if err != nil {
		t.Fatalf("parseBlockyMetrics returned error: %v", err)
	}

	if snapshot.TotalQueries != 65118 {
		t.Fatalf("TotalQueries = %d, want 65118", snapshot.TotalQueries)
	}

	if snapshot.BlockedQueries != 329 {
		t.Fatalf("BlockedQueries = %d, want 329", snapshot.BlockedQueries)
	}

	if snapshot.DomainsBlocked != 153812 {
		t.Fatalf("DomainsBlocked = %d, want 153812", snapshot.DomainsBlocked)
	}
}
