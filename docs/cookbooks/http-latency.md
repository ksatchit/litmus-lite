# Cookbook: HTTP latency

```
litmus-lite hub import http.latency -beside .
# edit listen/upstream in the file
litmus-lite validate ./http-latency.chaos.yaml
litmus-lite run ./http-latency.chaos.yaml -out run.json -html run.html
litmus-lite diagnose run.json
```

Point probes (and a load generator, if you use one) at `params.listen`, not `upstream`.
