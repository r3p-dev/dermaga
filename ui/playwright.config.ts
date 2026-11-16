import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 30000,
  use: {
    baseURL: "http://127.0.0.1:7070",
    screenshot: "only-on-failure",
  },
  webServer: {
    command:
      "sh -c 'mkdir -p /tmp/dermaga-e2e/data /tmp/dermaga-e2e/config /tmp/dermaga-e2e/run && ../bin/dermaga admin set-password --password correct-password-123 && ../bin/dermaga daemon'",
    url: "http://127.0.0.1:7070",
    reuseExistingServer: false,
    env: {
      XDG_DATA_HOME: "/tmp/dermaga-e2e/data",
      XDG_CONFIG_HOME: "/tmp/dermaga-e2e/config",
      XDG_RUNTIME_DIR: "/tmp/dermaga-e2e/run",
    },
  },
  projects: [
    {
      name: "firefox",
      use: { ...devices["Desktop Firefox"] },
    },
  ],
});
