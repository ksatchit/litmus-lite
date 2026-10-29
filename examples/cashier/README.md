# Cashier

Small Go service for the failpoint example and the README quick start. One route, `GET /charge`, calls `failpoint.Delay` and `failpoint.Fail`. Both stay dark until a scenario arms them.

Walkthrough: [failpoints](https://litmuschaos.github.io/litmus-lite/guides/failpoints/).

```
export LITMUS_LITE_FAILPOINT=charge,charge-slow
export LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo
go run ./examples/cashier
```

Listens on `127.0.0.1:8091`.
