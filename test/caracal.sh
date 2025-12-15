#!/bin/bash

# mock-caracal.sh
# Reads CSV input from stdin and outputs dummy responses to stdout

# Print header comment
echo "# capture_timestamp,probe_protocol,probe_src_addr,probe_dst_addr,probe_src_port,probe_dst_port,probe_ttl,quoted_ttl,reply_src_addr,reply_protocol,reply_icmp_type,reply_icmp_code,reply_ttl,reply_size,reply_mpls_labels,rtt,round"

# Read input lines and generate fake responses
while IFS=',' read -r dst_addr src_port dst_port ttl protocol; do
	# Skip empty lines
	[ -z "$dst_addr" ] && continue

	# Generate timestamp
	timestamp=$(date +%s)

	# Generate fake reply address based on TTL
	reply_addr="10.0.0.$ttl"

	# Generate fake RTT (1000-5000 microseconds)
	rtt=$((1000 + RANDOM % 4000))

	# Map protocol name to number
	case "$protocol" in
	icmp) proto_num=1 ;;
	tcp) proto_num=6 ;;
	udp) proto_num=17 ;;
	icmp6) proto_num=58 ;;
	*) proto_num=1 ;;
	esac

	# Output response
	echo "$timestamp,$proto_num,::ffff:192.168.1.1,::ffff:$dst_addr,$src_port,$dst_port,$ttl,64,::ffff:$reply_addr,1,11,0,64,56,[],$rtt,1"

done
