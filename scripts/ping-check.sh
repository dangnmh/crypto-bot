bash -c '
format="%-18s | %-8s | %-8s | %-8s | %-10s | %-14s\n"
echo "==================================================================================="
echo "📊 SERVER LOCATION: $(curl -s https://ipinfo.io/city 2>/dev/null || echo "Unknown"), $(curl -s https://ipinfo.io/country 2>/dev/null || echo "Unknown") (IP: $(curl -s https://ifconfig.me 2>/dev/null || echo "Unknown"))"
echo "==================================================================================="
printf "$format" "EXCHANGE" "DNS (ms)" "TCP (ms)" "TLS (ms)" "COLD (ms)" "HOT REUSE (ms)"
echo "-----------------------------------------------------------------------------------"

test_target() {
    name="$1"
    url="$2"
    res=$(curl -s \
      -w "%{time_namelookup} %{time_connect} %{time_appconnect} %{time_total}\n" -o /dev/null "$url" \
      --next -s \
      -w "%{time_total}\n" -o /dev/null "$url")
    
    dns=$(echo "$res" | head -n 1 | awk "{printf \"%.1f\", \$1 * 1000}")
    tcp=$(echo "$res" | head -n 1 | awk "{printf \"%.1f\", \$2 * 1000}")
    tls=$(echo "$res" | head -n 1 | awk "{printf \"%.1f\", (\$3 - \$2) * 1000}")
    cold=$(echo "$res" | head -n 1 | awk "{printf \"%.1f\", \$4 * 1000}")
    hot=$(echo "$res" | tail -n 1 | awk "{printf \"%.1f\", \$1 * 1000}")
    
    printf "$format" "$name" "$dns" "$tcp" "$tls" "$cold" "$hot"
}

test_target "MEXC (Contract)" "https://contract.mexc.com/api/v1/contract/ping"
test_target "MEXC (API)"      "https://api.mexc.com/api/v1/contract/ping"
test_target "Bybit"           "https://api.bybit.com/v5/market/time"
test_target "Binance Futures" "https://fapi.binance.com/fapi/v1/ping"
echo "==================================================================================="
'
