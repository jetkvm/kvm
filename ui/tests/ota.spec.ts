import { test, expect } from '@playwright/test';

import { m } from '../localization/paraglide/messages';

import { getByRole, getByText } from './helper';

const TARGET_DEVICE_IP = "192.168.2.4"

test('upgrade process', async ({ page }) => {
  await page.goto(`http://${TARGET_DEVICE_IP}`);

  // Expect a title "to contain" a substring.
  await expect(page).toHaveTitle(/JetKVM/);

  // Except ICE gathering completed or JetKVM device connected
  await expect(getByText(page, 'ice_gathering_completed').or(getByText(page, 'peer_connection_connected'))).toBeVisible();

  // Except No HDMI signal detected, as the device is not connected to the HDMI port
  await expect(getByText(page, 'video_overlay_no_hdmi_signal')).toBeVisible();

  // Emulate upgrade process
  await getByRole(page, 'button', 'action_bar_settings').click();
  await expect(getByText(page, 'general_page_description')).toBeVisible();

  const btnCheckForUpdates = getByRole(page, 'button', 'general_check_for_updates');
  await expect(btnCheckForUpdates).toBeVisible();
  await btnCheckForUpdates.click();

  // if System is to update, we'll then skip the update process and go to the next step
  if (await getByText(page, 'general_update_up_to_date_title').isVisible()) {
    test.skip(true, 'System is up to date, skipping update process');
    return;
  }

  // LoadingState -> Checking for updates...
  await expect(getByText(page, 'general_update_checking_title')).toBeVisible();

  // UpdateAvailableState -> Prompt to update the device
  await expect(getByText(page, 'general_update_available_title')).toBeVisible();
  const btnUpdateNow = getByRole(page, 'button', 'general_update_now_button');
  await expect(btnUpdateNow).toBeVisible();
  await btnUpdateNow.click();

  // LoadingState -> Updating your device...
  await expect(getByText(page, 'general_update_updating_title')).toBeVisible();
  const update_type = m.general_update_application_type();
  await expect(getByText(page, 'general_update_status_downloading', { update_type })).toBeVisible();
  await expect(getByText(page, 'general_update_status_verifying', { update_type })).toBeVisible();
  await expect(getByText(page, 'general_update_status_installing', { update_type })).toBeVisible();
  await expect(getByText(page, 'general_update_status_awaiting_reboot')).toBeVisible();

  await page.waitForTimeout(30000);
  // await expect(page.getByText('Update available')).toBeVisible();

  // await page.getByRole('button', { name: 'Update now' }).click();

  // await expect(page.getByText('Updating...')).toBeVisible();

});

test('downgrade process', async ({ page }) => {
  await page.goto(`http://${TARGET_DEVICE_IP}`);

  // Expect a title "to contain" a substring.
  await expect(
    page,
    'Title should include JetKVM',
  ).toHaveTitle(/JetKVM/);

  // Except ICE gathering completed or JetKVM device connected
  await expect(
    getByText(page, 'ice_gathering_completed').or(getByText(page, 'peer_connection_connected')),
    'should be ICE gathering completed or JetKVM device connected',
  ).toBeVisible();

  // Except No HDMI signal detected, as the device is not connected to the HDMI port
  await expect(
    getByText(page, 'video_overlay_no_hdmi_signal'),
    'should be visible when no HDMI signal is detected',
  ).toBeVisible();

  // Emulate upgrade process
  await getByRole(page, 'button', 'action_bar_settings').click();
  await expect(
    getByText(page, 'general_page_description'),
    'should be visible when general page description is visible',
  ).toBeVisible();

  // Go to Advanced tab
  const btnAdvanced = getByRole(page, 'link', 'settings_advanced');
  await expect(
    btnAdvanced,
    'should be visible when advanced link is visible',
  ).toBeVisible();
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
  await whatToUpdateSelect.selectOption('system');

  // now, make sure the system version input is visible
  const systemVersionLabel = page.locator(
    'div',
    { hasText: m.advanced_version_update_system_label(), hasNotText: m.advanced_version_update_target_label() },
  );
  await expect(
    systemVersionLabel,
    'SystemVersionLabel should be visible',
  ).toBeVisible();
  const systemVersionInput = systemVersionLabel.getByRole('textbox');
  await expect(
    systemVersionInput,
    'SystemVersionInput should be visible',
  ).toBeVisible();
  await systemVersionInput.fill('0.2.7');

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
  await btnVersionUpdate.click();

  // Upgrade is a very time-consuming process, so we'll give it a generous timeout
  const timeout = 5 * 60 * 1000;

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
  await btnUpdateNow.click();

  // LoadingState -> Updating your device...
  await expect(getByText(page, 'general_update_updating_title'),
    'UpdatingDeviceState: title should be updating your device...',
  ).toBeVisible();
  const update_type = m.general_update_system_type();

  // UpdatingDeviceState -> Downloading, Verifying, Installing, Awaiting reboot or rebooting
  await expect(
    getByText(page, 'general_update_status_downloading', { update_type }).
    or(getByText(page, 'general_update_status_verifying', { update_type })).
    or(getByText(page, 'general_update_status_installing', { update_type })),
    'UpdatingDeviceState: downloading, verifying, installing',
  ).toBeVisible({ timeout });
  await expect(
    getByText(page, 'general_update_status_awaiting_reboot')
    .or(getByText(page, 'general_update_rebooting')),
    'UpdatingDeviceState: awaiting reboot or rebooting',
  ).toBeVisible({ timeout });

  // Leaving device on
  // await expect(pageGetByText('updating_leave_device_on')).toBeVisible({ timeout });

  // Rebooting or different IP message
  await expect(
    getByText(page, 'video_overlay_reboot_device_is_rebooting')
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
});
