// VegaLoad scenario for LaunchPad (https://github.com/umamukkara/launchpad).
// Point at the litmus-lite HTTP proxy listen address during a resilience run
// (default http://127.0.0.1:18080), not the upstream, so load sees the fault.
export default function () {
  const base = __ENV.LAUNCHPAD_URL || "http://127.0.0.1:18080";
  http.get(base + "/healthz");
  http.get(base + "/api/launches");
  http.post(base + "/api/launches", {
    body: JSON.stringify({
      vehicle: "vega-c",
      target_thrust_kn: 2150,
      duration_seconds: 5,
    }),
    headers: { "Content-Type": "application/json" },
  });
}
