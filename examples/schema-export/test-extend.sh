#!/bin/bash
cd $(dirname "$0")

# Create temporary main with ExtendQueryNamespace = true
cat main.go | sed 's/config.ExtendQueryNamespace = false/config.ExtendQueryNamespace = true/' > /tmp/main-extend.go

# Run it
cd /tmp && go run main-extend.go 2>&1 | grep -B 1 -A 4 "type Leads"
