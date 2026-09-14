import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { deviceShellAvailable, resetConfigViaSSH, restartAppViaSSH, sshExec } from "./ssh";

const ANIMATION_DELAY = 150;

// Known test passwords - used when device is in unknown state and needs login
const KNOWN_TEST_PASSWORDS = [
  ...new Set(
    [process.env.JETKVM_PASSWORD, "TestPassword123", "NewPassword456"].filter(
      (p): p is string => !!p,
    ),
  ),
];

async function requireAuthRecoveryShell(): Promise<void> {
  if (!(await deviceShellAvailable())) {
    throw new Error(
      "Cannot authenticate to device; set JETKVM_PASSWORD to its current password. Device SSH recovery is unavailable.",
    );
  }
}

/**
 * Reset the device to onboarding/welcome state via SSH.
 * Prefer ensureLocalAuthMode() unless testing the welcome flow itself.
 */
export async function resetDeviceToWelcome(page: Page): Promise<void> {
  await page.goto("/");
  await page.waitForLoadState("networkidle");

  const currentUrl = page.url();
  if (currentUrl.includes("/welcome")) {
    if (!currentUrl.endsWith("/welcome")) {
      await page.goto("/welcome");
    }
    await page.waitForTimeout(ANIMATION_DELAY);
    return;
  }

  await resetConfigViaSSH();
  await restartAppViaSSH();
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(ANIMATION_DELAY);
}

export async function goToWelcomeMode(page: Page): Promise<void> {
  const setupButton = page.getByRole("link", { name: /Set up your JetKVM/i });
  await expect(setupButton).toBeVisible({ timeout: 10000 });
  await setupButton.click();

  await page.waitForURL("**/welcome/mode", { timeout: 10000 });
}

export async function selectWelcomeAuthMode(
  page: Page,
  mode: "password" | "noPassword",
): Promise<void> {
  const radio = page.locator(`input[type="radio"][value="${mode}"]`);
  await expect(radio).toBeVisible({ timeout: 5000 });
  await radio.click();

  const continueButton = page.getByRole("button", { name: /Continue/i });
  await expect(continueButton).toBeEnabled({ timeout: 5000 });
  await continueButton.click();
}

export async function submitWelcomePassword(
  page: Page,
  password: string,
  confirmPassword?: string,
  expectSuccess = true,
): Promise<void> {
  await page.waitForURL("**/welcome/password", { timeout: 10000 });

  const passwordInput = page.locator('input[name="password"]');
  const confirmPasswordInput = page.locator('input[name="confirmPassword"]');

  await passwordInput.fill(password);
  await confirmPasswordInput.fill(confirmPassword ?? password);

  const submitButton = page.getByRole("button", { name: /Set Password/i });
  await expect(submitButton).toBeEnabled({ timeout: 5000 });
  await submitButton.click();

  if (expectSuccess) {
    await page.waitForURL("/", { timeout: 15000 });
  } else {
    await page.waitForTimeout(200);
  }
}

export async function loginLocal(
  page: Page,
  password: string,
  expectSuccess = true,
): Promise<{ success: boolean; error?: string }> {
  const passwordInput = page.locator('input[name="password"]');
  await expect(passwordInput).toBeVisible({ timeout: 5000 });

  // Check if input is enabled (might be disabled due to rate-limiting)
  const isEnabled = await passwordInput.isEnabled({ timeout: 3000 }).catch(() => false);
  if (!isEnabled) {
    if (expectSuccess) {
      throw new Error("Login failed: password input is disabled (likely rate-limited)");
    }
    return { success: false, error: "Rate limited - input disabled" };
  }

  await passwordInput.fill(password, { timeout: 5000 });

  const submitButton = page.getByRole("button", { name: /Log in/i });
  const submitEnabled = await submitButton.isEnabled({ timeout: 3000 }).catch(() => false);
  if (!submitEnabled) {
    if (expectSuccess) {
      throw new Error("Login failed: submit button is disabled");
    }
    return { success: false, error: "Submit button disabled" };
  }
  await submitButton.click();

  // Race between successful navigation and error message appearance so failed
  // logins resolve quickly (~500ms) instead of waiting for the full URL timeout.
  const errorLocator = page.locator(".text-red-500, .text-red-600").first();
  const outcome = await Promise.race([
    page
      .waitForURL(url => !url.toString().includes("/login"), {
        timeout: 5000,
      })
      .then(() => "navigated" as const),
    errorLocator.waitFor({ state: "visible", timeout: 5000 }).then(() => "error" as const),
  ]).catch(() => "timeout" as const);

  if (outcome === "navigated") {
    return { success: true };
  }

  const errorText = await errorLocator.textContent({ timeout: 1000 }).catch(() => null);

  if (expectSuccess) {
    // Test expected success but login failed
    throw new Error(`Login failed: ${errorText || "Unknown error"}`);
  }

  return { success: false, error: errorText || undefined };
}

export async function logout(page: Page): Promise<void> {
  await page.evaluate(async () => {
    await fetch("/auth/logout", { method: "POST" });
  });
  await page.waitForTimeout(100);
}

export async function dismissSessionTakeoverDialog(page: Page): Promise<void> {
  const useHereButton = page.getByRole("button", { name: /Use Here/i });
  if (await useHereButton.isVisible({ timeout: 2000 }).catch(() => false)) {
    await useHereButton.click();
    await page.waitForTimeout(200);
  }
}

/** Wait for the auth loader to render a terminal local page, not network idle. */
export async function waitForLocalAuthPage(page: Page): Promise<void> {
  await page
    .locator('input[name="password"], a[href="/welcome/mode"], video')
    .first()
    .waitFor({ state: "attached", timeout: 15000 });
}

export async function openAccessSettings(page: Page): Promise<void> {
  await page.goto("/settings/access");
  await dismissSessionTakeoverDialog(page);

  // Wait for the local auth section to appear (indicates loaderData is loaded)
  const localSectionHeader = page.locator("text=Authentication Mode");
  await expect(localSectionHeader).toBeVisible({ timeout: 15000 });
}

export async function enablePasswordFromSettings(
  page: Page,
  password: string,
  confirmPassword?: string,
  expectSuccess = true,
): Promise<void> {
  const enablePasswordButton = page.getByRole("button").filter({ hasText: /Enable Password/i });
  await expect(enablePasswordButton).toBeVisible({ timeout: 10000 });
  await enablePasswordButton.click();

  // Wait for modal to appear
  const passwordInput = page.locator('input[type="password"]').first();
  await expect(passwordInput).toBeVisible({ timeout: 5000 });

  const confirmPasswordInput = page.locator('input[type="password"]').nth(1);
  await passwordInput.fill(password);
  await confirmPasswordInput.fill(confirmPassword ?? password);

  const secureButton = page.getByRole("button", {
    name: /Secure|Set Password/i,
  });
  await secureButton.click();

  if (expectSuccess) {
    const successMessage = page.locator("text=Password Set Successfully");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();
  }
}

export async function changePasswordFromSettings(
  page: Page,
  oldPassword: string,
  newPassword: string,
  confirmNewPassword?: string,
  expectSuccess = true,
): Promise<void> {
  const changePasswordButton = page.getByRole("button").filter({ hasText: /Change Password/i });
  await expect(changePasswordButton).toBeVisible({ timeout: 10000 });
  await changePasswordButton.click();

  // Wait for modal to appear
  const oldPasswordInput = page.locator('input[type="password"]').first();
  await expect(oldPasswordInput).toBeVisible({ timeout: 5000 });

  const newPasswordInput = page.locator('input[type="password"]').nth(1);
  const confirmNewPasswordInput = page.locator('input[type="password"]').nth(2);

  await oldPasswordInput.fill(oldPassword);
  await newPasswordInput.fill(newPassword);
  await confirmNewPasswordInput.fill(confirmNewPassword ?? newPassword);

  const updateButton = page.getByRole("button", { name: /Update Password/i });
  await updateButton.click();

  if (expectSuccess) {
    const successMessage = page.locator("text=Password Updated Successfully");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();
  }
}

export async function disablePasswordFromSettings(
  page: Page,
  currentPassword: string,
  expectSuccess = true,
): Promise<void> {
  const disableButton = page.getByRole("button").filter({ hasText: /Disable Protection/i });
  await expect(disableButton).toBeVisible({ timeout: 10000 });
  await disableButton.click();

  // Wait for modal to appear
  const passwordInput = page.locator('input[type="password"]').first();
  await expect(passwordInput).toBeVisible({ timeout: 5000 });
  await passwordInput.fill(currentPassword);

  const confirmDisableButton = page.getByRole("button", {
    name: /Disable.*Protection/i,
  });
  await confirmDisableButton.click();

  if (expectSuccess) {
    const successMessage = page.locator("text=Password Protection Disabled");
    await expect(successMessage).toBeVisible({ timeout: 5000 });

    const closeButton = page.getByRole("button", { name: /Close/i });
    await closeButton.click();
  }
}

export type LocalAuthModeConfig = { mode: "noPassword" } | { mode: "password"; password: string };

/**
 * Ensure the device is in the desired local auth mode.
 * Handles welcome, login, and already-configured states transparently.
 */
export async function ensureLocalAuthMode(page: Page, desired: LocalAuthModeConfig): Promise<void> {
  await page.goto("/");
  await waitForLocalAuthPage(page);

  const currentUrl = page.url();

  if (currentUrl.includes("/welcome")) {
    // Device is in onboarding mode - complete setup
    await goToWelcomeMode(page);
    if (desired.mode === "noPassword") {
      await selectWelcomeAuthMode(page, "noPassword");
      await page.waitForURL("/", { timeout: 15000 });
    } else {
      await selectWelcomeAuthMode(page, "password");
      await submitWelcomePassword(page, desired.password);
    }
    return;
  }

  if (currentUrl.includes("/login")) {
    // Device has password protection - try to login with known passwords
    const passwordsToTry =
      desired.mode === "password"
        ? [desired.password, ...KNOWN_TEST_PASSWORDS.filter(p => p !== desired.password)]
        : [...KNOWN_TEST_PASSWORDS];

    let loggedIn = false;
    let usedPassword: string | null = null;
    for (const pwd of passwordsToTry) {
      const result = await loginLocal(page, pwd, false);
      if (result.success) {
        loggedIn = true;
        usedPassword = pwd;
        break;
      }
      // Re-navigate to login if needed (page may have changed)
      if (!page.url().includes("/login")) break;
    }

    if (loggedIn) {
      if (desired.mode === "password" && usedPassword === desired.password) {
        return; // Already has correct password
      }
      if (desired.mode === "password") {
        // Change password via settings UI
        await openAccessSettings(page);
        await changePasswordFromSettings(page, usedPassword!, desired.password);
        return;
      }
      // desired.mode === "noPassword" - disable via settings UI
      await openAccessSettings(page);
      await disablePasswordFromSettings(page, usedPassword!);
      return;
    }

    await requireAuthRecoveryShell();
    await resetConfigViaSSH();
    await restartAppViaSSH();
    await page.goto("/");
    await page.waitForLoadState("networkidle");
    if (desired.mode === "password") {
      await goToWelcomeMode(page);
      await selectWelcomeAuthMode(page, "password");
      await submitWelcomePassword(page, desired.password);
    } else {
      await goToWelcomeMode(page);
      await selectWelcomeAuthMode(page, "noPassword");
      await page.waitForURL("/", { timeout: 15000 });
    }
    return;
  }

  // Fresh browser context at "/" with no cookies and no redirect means no password is set.
  if (desired.mode === "noPassword") {
    return;
  }

  // Device is configured and we're logged in (or no password) - check current mode
  await openAccessSettings(page);

  const hasDisableButton = await page
    .getByRole("button")
    .filter({ hasText: /Disable Protection/i })
    .isVisible({ timeout: 2000 })
    .catch(() => false);

  if (desired.mode === "password") {
    if (hasDisableButton) {
      // Already has password - nothing to do (we're already logged in)
      return;
    }
    // No password currently - enable it
    await enablePasswordFromSettings(page, desired.password);
  } else {
    if (!hasDisableButton) {
      // Already no password - nothing to do
      return;
    }
    // Has password - try disabling with known passwords
    for (const pwd of KNOWN_TEST_PASSWORDS) {
      try {
        await disablePasswordFromSettings(page, pwd);
        return;
      } catch {
        // Wrong password, try next
        await openAccessSettings(page);
      }
    }
    // Fall back only when a device shell is available.
    await requireAuthRecoveryShell();
    await clearPasswordViaSSH();
    await page.goto("/");
    await page.waitForLoadState("networkidle");
  }
}

/** Clear password fields from device config via SSH (keeps device configured). */
export async function clearPasswordViaSSH(): Promise<void> {
  try {
    // Run separate sed commands to avoid complex quoting issues
    // Note: JSON has space after colon, e.g. "key": "value"
    // Clear hashed_password
    await sshExec(
      'sed -i "s/\\"hashed_password\\": \\"[^\\"]*\\"/\\"hashed_password\\": \\"\\"/g" /userdata/kvm_config.json',
    );
    // Clear local_auth_token
    await sshExec(
      'sed -i "s/\\"local_auth_token\\": \\"[^\\"]*\\"/\\"local_auth_token\\": \\"\\"/g" /userdata/kvm_config.json',
    );
    // Set localAuthMode to noPassword (note: camelCase in JSON)
    await sshExec(
      'sed -i "s/\\"localAuthMode\\": \\"[^\\"]*\\"/\\"localAuthMode\\": \\"noPassword\\"/g" /userdata/kvm_config.json',
    );

    await restartAppViaSSH();
  } catch (error) {
    console.error("[E2E Cleanup] Error clearing password:", error);
    throw error; // Don't swallow errors silently
  }
}

export async function triggerRateLimit(page: Page, maxAttempts = 10): Promise<boolean> {
  for (let i = 0; i < maxAttempts; i++) {
    const result = await loginLocal(page, "wrongpassword123", false);

    if (result.error && /too many|rate.?limit|try again/i.test(result.error)) {
      return true;
    }

    await page.waitForTimeout(100);
  }

  return false;
}
