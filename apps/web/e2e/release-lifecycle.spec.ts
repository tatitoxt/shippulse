import { expect, test, type Page } from "@playwright/test";

const PASSWORD = process.env.DEMO_PASSWORD ?? "shippulse-demo";
// Unique versions per run so the suite can be re-run against the same stack.
const run = Date.now().toString(36);
const GOOD = `v2.0.0-${run}`;
const BAD = `v2.1.0-${run}`;

const nav = (page: Page, name: string) => page.getByRole("navigation").getByRole("link", { name, exact: true });

async function login(page: Page, email: string) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByTestId("whoami")).toContainText(email);
}

async function deploy(page: Page, version: string, profile: string): Promise<string> {
  await nav(page, "Services").click();
  await page.getByLabel("Service").selectOption({ label: "catalog-api" });
  await page.getByLabel("Environment").selectOption({ label: "staging" });
  await page.getByLabel("Version").fill(version);
  await page.getByLabel("Simulated outcome").selectOption(profile);
  await page.getByRole("button", { name: "Deploy" }).click();
  await page.waitForURL(/\/deployments\/[0-9a-f-]{36}$/);
  return page.url();
}

test("healthy deploy is promoted; unhealthy deploy is rolled back automatically and survives a reload", async ({ page }) => {
  await login(page, "operator@acme.example");

  await deploy(page, GOOD, "healthy");
  await expect(page.getByTestId("deployment-status")).toHaveText("succeeded");
  for (const step of ["validate", "apply_version", "health_probe", "promote"]) {
    await expect(page.getByTestId(`step-${step}-status`)).toHaveText("succeeded");
  }
  await expect(page.getByTestId("runtime-evidence").getByTestId("observed-version")).toHaveText(GOOD);

  const failedUrl = await deploy(page, BAD, "unhealthy");
  await expect(page.getByTestId("deployment-status")).toHaveText("failed");
  await expect(page.getByTestId("failure")).toContainText("health_check_failed");
  await expect(page.getByTestId("step-health_probe-status")).toHaveText("failed");
  await expect(page.getByTestId("step-promote-status")).toHaveText("skipped");
  await expect(page.getByTestId("rollback-automatic")).toContainText(`${BAD} → ${GOOD}`);
  await expect(page.getByTestId("rollback-automatic-status")).toHaveText("succeeded");

  // Evidence comes from PostgreSQL and the simulated runtime, so it survives a full reload.
  await page.reload();
  await expect(page).toHaveURL(failedUrl);
  await expect(page.getByTestId("deployment-status")).toHaveText("failed");
  await expect(page.getByTestId("rollback-automatic-status")).toHaveText("succeeded");
  const evidence = page.getByTestId("runtime-evidence");
  await expect(evidence.getByTestId("stable-version")).toHaveText(GOOD);
  await expect(evidence.getByTestId("observed-version")).toHaveText(GOOD);
  const audit = page.getByTestId("audit-trail");
  for (const action of ["deployment.requested", "deployment.failed", "rollback.requested", "rollback.succeeded"]) {
    await expect(audit).toContainText(action);
  }

  await nav(page, "Deployments").click();
  await expect(page.getByTestId(`deployment-row-${BAD}`)).toContainText("failed");
  await expect(page.getByTestId(`deployment-row-${BAD}`)).toContainText(GOOD);

  await nav(page, "Services").click();
  const env = page.getByTestId("env-catalog-api-staging");
  await expect(env.getByTestId("stable-version")).toHaveText(GOOD);
  await expect(env.getByTestId("observed-version")).toContainText(GOOD);
});

test("viewer can inspect but is denied deploys in the UI and by the API", async ({ page }) => {
  await login(page, "viewer@acme.example");
  await nav(page, "Services").click();
  await expect(page.getByTestId("viewer-notice")).toBeVisible();
  await expect(page.getByRole("button", { name: "Deploy" })).toBeDisabled();

  const token = await page.evaluate(() => window.localStorage.getItem("shippulse.token"));
  const services = await (await page.request.get("/api/v1/services", { headers: { Authorization: `Bearer ${token}` } })).json();
  const svc = services.services[0];
  const res = await page.request.post("/api/v1/deployments", {
    headers: { Authorization: `Bearer ${token}`, "Idempotency-Key": `viewer-${run}` },
    data: { service_id: svc.id, environment_id: svc.environments[0].environment_id, version: "v9.9.9" },
  });
  expect(res.status()).toBe(403);
  expect((await res.json()).error.code).toBe("forbidden");
});

test("another tenant cannot see Acme services or deployments", async ({ page }) => {
  await login(page, "operator@acme.example");
  await nav(page, "Deployments").click();
  const acmeDeployment = await page.getByTestId("deployments-table").locator("a").first().getAttribute("href");
  expect(acmeDeployment).toMatch(/^\/deployments\//);
  await page.getByRole("button", { name: "Sign out" }).click();

  await login(page, "operator@globex.example");
  await nav(page, "Services").click();
  await expect(page.getByTestId("service-invoice-worker")).toBeVisible();
  await expect(page.getByTestId("service-catalog-api")).toHaveCount(0);
  await page.goto(acmeDeployment!);
  await expect(page.getByTestId("not-found")).toBeVisible();
});
