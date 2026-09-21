import { enUS } from "../src/i18n/en-US";
import { expect, test } from "./fixtures";

/**
 * The SMS-code sign-in toggle on the sign-in screen (Task 15).
 *
 * SMSLoginEnabled cannot be turned on at all unless the deployment can
 * actually send SMS (settings.go refuses the setting otherwise), which is
 * why playwright.config.ts's webServer now carries fake
 * PORTICO_ALIYUN_SMS_* credentials -- see the comment there. Those
 * credentials only have to exist, not work: this test never types a phone
 * number bound to a real account, so SMSLoginService.completeRequest
 * returns at its "no such phone in this tenant" branch before it ever
 * calls SMSSender.Send, and no request reaches Aliyun.
 *
 * That is also why this test does not attempt to complete a sign-in with a
 * code -- there is nowhere in this suite to read the code that would have
 * been sent (no equivalent of Mailpit for SMS), and the brief's three
 * assertions (the toggle swaps the form, the send button disables with a
 * countdown) do not require one.
 */

test("switching to SMS code sign-in swaps the password form for phone/code fields, and requesting a code disables the send button with a countdown", async ({
  page,
  signIn,
}) => {
  // Turning SMS login on is itself the tenant's own admin action, so it is
  // done the same way an administrator would: through the Settings screen,
  // not by writing to the database out from under the app.
  await signIn();
  await page.goto("/settings");

  const smsToggle = page.getByLabel(enUS["settings.smsLoginEnabled"]);
  await expect(smsToggle).not.toBeChecked();
  await smsToggle.check();
  await page.getByRole("button", { name: enUS["common.save"] }).click();
  await expect(page.getByText(enUS["settings.saved"])).toBeVisible();

  try {
    // Drops the stored session rather than using the account menu's sign-out:
    // what this test needs is a signed-out visitor looking at /login, not a
    // server-side session end, and clearing the token is the same thing the
    // other authenticated-screen tests in this suite rely on the fixture's
    // signIn to have set in the first place.
    await page.evaluate(() => localStorage.removeItem("portico.token"));
    await page.goto("/login");

    await expect(
      page.getByRole("heading", { name: enUS["login.title"] }),
    ).toBeVisible();
    await expect(page.getByLabel(enUS["login.password"])).toBeVisible();

    await page
      .getByRole("button", { name: enUS["login.useSmsCode"] })
      .click();

    // The password form is gone, not just hidden -- the two forms are a
    // mode switch, not an accordion.
    await expect(page.getByLabel(enUS["login.password"])).toHaveCount(0);
    await expect(page.getByLabel(enUS["login.phone"])).toBeVisible();
    await expect(page.getByLabel(enUS["login.smsCode"])).toBeVisible();

    const sendButton = page.getByRole("button", {
      name: enUS["login.smsSend"],
    });
    // Disabled with no phone typed yet -- see LoginPage.tsx's
    // `smsPhone.trim() === ""` guard.
    await expect(sendButton).toBeDisabled();

    await page.getByLabel(enUS["login.phone"]).fill("+15550000000");
    await expect(sendButton).toBeEnabled();
    await sendButton.click();

    // Once the request resolves, the button relabels itself with a
    // counting-down resend time and disables again -- asserted through the
    // "{0}" placeholder's shape, not a literal number, since the countdown
    // is ticking by the time this assertion runs.
    const resendButton = page.getByRole("button", { name: /Resend in \d+s/ });
    await expect(resendButton).toBeVisible();
    await expect(resendButton).toBeDisabled();
  } finally {
    // Turn the setting back off no matter how the assertions above came
    // out: this suite runs with workers: 1 against one server and one
    // database, so a test after this one that only sets up a client-side
    // session must not find SMS login still on.
    await signIn();
    await page.goto("/settings");
    await page.getByLabel(enUS["settings.smsLoginEnabled"]).uncheck();
    await page.getByRole("button", { name: enUS["common.save"] }).click();
    await expect(page.getByText(enUS["settings.saved"])).toBeVisible();
  }
});
