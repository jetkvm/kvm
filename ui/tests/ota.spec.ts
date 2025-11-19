import { test, expect, PlaywrightTestArgs, Page } from '@playwright/test';

import { m } from '../localization/paraglide/messages';

import { getByRole, getByText, sleep } from './helper';

const TARGET_DEVICE_IP = "192.168.0.145"

interface CustomUpgradeProcessOptions {
  sys?: string;
  app?: string;
  resetConfig?: boolean;
}

const doCustomUpdateSelect = async (page: Page, sys?: string, app?: string) => {
  // Go to Advanced tab
  const btnAdvanced = getByRole(page, 'link', 'settings_advanced');
  await expect(
    btnAdvanced,
    'should be visible when advanced link is visible',
  ).toBeVisible();
  await sleep();
  await btnAdvanced.click();

  // Now we're on the Advanced tab
  await expect(
    getByText(page, 'advanced_version_update_title'),
    'should be visible when advanced version update title is visible',
  ).toBeVisible();

  // choose the System only option
  const whatToUpdateLabel = page.locator('label', { hasText: m.advanced_version_update_target_label() });
  await expect(
    whatToUpdateLabel,
    'should be visible when what to update label is visible',
  ).toBeVisible();
  const whatToUpdateSelect = page.locator('select');
  await expect(
    whatToUpdateSelect,
    'should be visible when what to update select is visible',
  ).toBeVisible();
  if (sys && app) {
    await whatToUpdateSelect.selectOption('both');
  } else if (sys) {
    await whatToUpdateSelect.selectOption('system');
  } else if (app) {
    await whatToUpdateSelect.selectOption('app');
  }

  if (sys) {
    // now, make sure the system version input is visible
    const systemVersionLabel = page.locator(
      'div',
      { hasText: m.advanced_version_update_system_label(), hasNotText: m.advanced_version_update_target_label() },
    );
    await expect(systemVersionLabel, 'SystemVersionLabel should be visible').toBeVisible();
    const systemVersionInput = systemVersionLabel.getByRole('textbox');
    await expect(systemVersionInput, 'SystemVersionInput should be visible').toBeVisible();
    await systemVersionInput.fill(sys);
  }

  if (app) {
    const appVersionLabel = page.locator(
      'div',
      { hasText: m.advanced_version_update_app_label(), hasNotText: m.advanced_version_update_target_label() },
    );
    await expect(appVersionLabel, 'AppVersionLabel should be visible').toBeVisible();
    const appVersionInput = appVersionLabel.getByRole('textbox');
    await expect(appVersionInput, 'AppVersionInput should be visible').toBeVisible();
    await appVersionInput.fill(app);
  }

  // acknowledge the version change
  const versionChangeAcknowledgedLabel = page.locator(
    'label',
    { hasText: "I understand version changes may break my device and require factory reset" },
  );
  await expect(
    versionChangeAcknowledgedLabel,
  ).toBeVisible();
  const versionChangeAcknowledgedCheckbox = versionChangeAcknowledgedLabel.getByRole('checkbox');
  await expect(versionChangeAcknowledgedCheckbox).toBeVisible();
  await versionChangeAcknowledgedCheckbox.check();

  // now, click the damn button
  const btnVersionUpdate = getByRole(page, 'button', 'advanced_version_update_button');
  await expect(btnVersionUpdate).toBeVisible();
  await sleep();
  await btnVersionUpdate.click();

  // wait for 1 minute for the checkUpdate process to complete
  const timeout = 1 * 60 * 1000;

  // LoadingState -> Checking for updates...
  await expect(getByText(page, 'general_update_checking_title'),
    'UpdatingDeviceState: checking for updates...',
  ).toBeVisible({ timeout });

  // UpdateAvailableState -> Prompt to update the device
  await expect(getByText(page, 'general_update_available_title'),
    'should be visible when general update available title is visible',
  ).toBeVisible({ timeout });
  await expect(getByText(page, 'general_update_will_disable_auto_update_description'),
    'UpdateAvailableState: should show warning if it\'s a custom update',
  ).toBeVisible();

  const btnUpdateNow = getByRole(page, 'button', 'general_update_now_button');
  await expect(btnUpdateNow,
    'Update Now button should be visible',
  ).toBeVisible();
  await sleep();
  await btnUpdateNow.click();
}

const doStandardUpdate = async (page: Page) => {
  const btnCheckForUpdates = getByRole(page, 'button', 'general_check_for_updates');
  await expect(btnCheckForUpdates).toBeVisible();
  await sleep();
  await btnCheckForUpdates.click();

  // LoadingState -> Checking for updates...
  // sometimes it would be too fast to check for updates, so we just check both titles here
  await expect(
    getByText(page, 'general_update_checking_title').
      or(getByText(page, 'general_update_available_title')).
      or(getByText(page, 'general_update_up_to_date_title')),
    'LoadingState: checking for updates or update available',
  ).toBeVisible();

  // check if system is up to date
  if (await getByText(page, 'general_update_up_to_date_title').isVisible()) {
    test.skip(true, 'System is up to date, skipping update process');
    return;
  }

  // UpdateAvailableState -> Prompt to update the device
  const btnUpdateNow = getByRole(page, 'button', 'general_update_now_button');
  await expect(btnUpdateNow).toBeVisible();

  // Check which updates are going to be installed
  const updateAvailableLabel = page.locator('.mb-2.text-sm', { hasText: m.general_update_available_description() }).first();
  await expect(updateAvailableLabel).toBeVisible();

  const updateAvailableContainer = page.locator('.text-left', { has: updateAvailableLabel }).first();
  await expect(updateAvailableContainer).toBeVisible();

  const versionInfo = {
    sys: '',
    app: '',
  }

  const systemUpdateAvailable = updateAvailableContainer.locator('.text-sm', { hasText: m.general_update_system_type() + ":" }).first();
  if (await systemUpdateAvailable.isVisible()) {
    versionInfo.sys = await systemUpdateAvailable.textContent() ?? '';
  }

  const appUpdateAvailable = updateAvailableContainer.locator('.text-sm', { hasText: m.general_update_application_type() + ":" }).first();
  if (await appUpdateAvailable.isVisible()) {
    versionInfo.app = await appUpdateAvailable.textContent() ?? '';
  }

  await sleep();
  await btnUpdateNow.click();

  return versionInfo;
}

const runUpdateTest = ({ sys, app }: CustomUpgradeProcessOptions) => async ({ page }: PlaywrightTestArgs) => {
  await page.goto(`http://${TARGET_DEVICE_IP}`);

  // Expect a title "to contain" a substring.
  await expect(
    page,
    'Title should include JetKVM',
  ).toHaveTitle(/JetKVM/);

  // If it's showing Welcome screen, we'll do the setup process,
  // otherwise, we'll skip the setup process and go to the next step
  await page.waitForLoadState('networkidle');

  if (await getByText(page, 'welcome_to_jetkvm').isVisible()) {
    // check if current URL is /welcome
    await page.waitForURL('**/welcome');
    // click the setup button (LinkButton)
    await getByRole(page, 'link', 'jetkvm_setup').click();
    // check if current URL is /welcome/mode
    await page.waitForURL('**/welcome/mode');
    // click the local authentication method button
    await page.locator('input[name="localAuthMode"][value="noPassword"]').click();
    // wait for 1 second to ensure the checkbox is checked
    await sleep();
    // click the continue button
    await getByRole(page, 'button', 'continue').click();
  }

  // Except ICE gathering completed or JetKVM device connected
  // await expect(
  //   getByText(page, 'ice_gathering_completed').
  //     or(getByText(page, 'video_overlay_loading_stream')).
  //     or(getByText(page, 'video_overlay_no_hdmi_signal'))
  //   ,
  //   'Wait until the WebRTC connection is established',
  // ).toBeVisible();
  await expect(
    getByText(page, 'video_overlay_no_hdmi_signal'),
    'Wait until the WebRTC connection is established',
  ).toBeVisible();

  await sleep();

  // Emulate upgrade process
  await getByRole(page, 'button', 'action_bar_settings').click();
  await expect(
    getByText(page, 'general_page_description'),
    'should be visible when general page description is visible',
  ).toBeVisible();

  let [sysUpdatePending, appUpdatePending] = [false, false];

  // If sys or app is provided, we'll do a custom update, otherwise, we'll do a standard update
  if (sys || app) {
    await doCustomUpdateSelect(page, sys, app);
    sysUpdatePending = sys !== undefined;
    appUpdatePending = app !== undefined;
  } else {
    const versionInfo = await doStandardUpdate(page);
    sysUpdatePending = versionInfo?.sys !== '';
    appUpdatePending = versionInfo?.app !== '';
  }

  // LoadingState -> Updating your device...
  await expect(getByText(page, 'general_update_updating_title'),
    'UpdatingDeviceState: title should be updating your device...',
  ).toBeVisible();

  // update is a very time-consuming process, so we'll give it a generous timeout
  const timeout = 10 * 60 * 1000; // 10 minutes

  // UpdatingDeviceState -> Downloading, Verifying, Installing, Awaiting reboot or rebooting
  const waitingForUpdate = async (update_type: string = m.general_update_system_type()) => {
    await expect(
      getByText(page, 'general_update_status_downloading', { update_type }).
        or(getByText(page, 'general_update_status_verifying', { update_type })).
        or(getByText(page, 'general_update_status_installing', { update_type })),
      'UpdatingDeviceState: downloading, verifying, installing',
    ).toBeVisible({ timeout });
    await expect(
      getByText(page, 'general_update_status_awaiting_reboot')
        .or(getByText(page, 'general_update_rebooting'))
      , 'UpdatingDeviceState: awaiting reboot or rebooting',
    ).toBeVisible({ timeout });
  };

  // Application update always comes first
  if (appUpdatePending) await waitingForUpdate(m.general_update_application_type());
  if (sysUpdatePending) await waitingForUpdate(m.general_update_system_type());

  // Leaving device on
  // await expect(pageGetByText('updating_leave_device_on')).toBeVisible({ timeout });

  // Rebooting or different IP message
  await expect(
    getByText(page, 'general_update_rebooting')
      .or(getByText(page, 'video_overlay_reboot_unable_to_reconnect'))
      .or(getByText(page, 'video_overlay_reboot_different_ip_message'))
    , 'VideoOverlay should show either reboot device is rebooting or different ip message'
  ).toBeVisible({ timeout });

  // Get the element indicating WebRTC connection status
  // TODO: use the role="status" to filter the element instead of the classNames
  const peerConnectionStatusCard = page.locator('.flex.items-center.gap-x-3.border.bg-white', { hasText: m.jetkvm_device() });
  // we're testing against the production build, so react locators aren't available here
  await expect(
    peerConnectionStatusCard,
    'PeerConnectionStatusCard should be visible'
  ).toBeVisible({ timeout });

  // it should transition to the disconnected state
  await expect(
    peerConnectionStatusCard.getByText(m.peer_connection_disconnected()),
    'PeerConnectionStatusCard should be transitioned to the DISCONNECTED state',
  ).toBeVisible({ timeout });

  // then, wait for the peer connection status card to transition to the CONNECTED state
  await expect(
    peerConnectionStatusCard.getByText(m.peer_connection_connected()),
    'PeerConnectionStatusCard should be transitioned to the CONNECTED state after reboot',
  ).toBeVisible({ timeout });

  // then, we should see a message saying that the update is completed successfully
  await expect(getByText(page, 'general_update_completed_title'),
    'UpdateCompletedState: title should be update completed successfully',
  ).toBeVisible({ timeout });
  await expect(getByText(page, 'general_update_completed_description'),
    'UpdateCompletedState: description should be update completed successfully',
  ).toBeVisible({ timeout });
};

test('standard upgrade process', runUpdateTest({}));

test('custom upgrade process: upgrade system only', runUpdateTest({
  sys: '0.2.7',
}));

test('custom upgrade process: upgrade app only', runUpdateTest({
  app: '0.4.8',
}));

test('custom upgrade process: upgrade both', runUpdateTest({
  sys: '0.2.7',
  app: '0.4.8',
}));