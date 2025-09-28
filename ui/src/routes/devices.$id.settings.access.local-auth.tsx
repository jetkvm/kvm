import { useState, useEffect } from "react";
import { useLocation, useRevalidator } from "react-router";
import { useTranslation } from "react-i18next";

import { Button } from "@components/Button";
import { InputFieldWithLabel } from "@/components/InputField";
import api from "@/api";
import { useLocalAuthModalStore } from "@/hooks/stores";
import { useDeviceUiNavigation } from "@/hooks/useAppNavigation";

export default function SecurityAccessLocalAuthRoute() {
  const { setModalView } = useLocalAuthModalStore();
  const { navigateTo } = useDeviceUiNavigation();
  const location = useLocation();
  const init = location.state?.init;

  useEffect(() => {
    if (!init) {
      navigateTo("..");
    } else {
      setModalView(init);
    }
  }, [init, navigateTo, setModalView]);

  {
    /* TODO: Migrate to using URLs instead of the global state. To simplify the refactoring, we'll keep the global state for now. */
  }
  return <Dialog onClose={() => navigateTo("..")} />;
}

export function Dialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const { modalView, setModalView } = useLocalAuthModalStore();
  const [error, setError] = useState<string | null>(null);
  const revalidator = useRevalidator();

  const handleCreatePassword = async (password: string, confirmPassword: string) => {
    if (password === "") {
      setError(t('Please_enter_a_password'));
      return;
    }

    if (password !== confirmPassword) {
      setError(t('Passwords_do_not_match'));
      return;
    }

    try {
      const res = await api.POST("/auth/password-local", { password });
      if (res.ok) {
        setModalView("creationSuccess");
        // The rest of the app needs to revalidate the device authMode
        revalidator.revalidate();
      } else {
        const data = await res.json();
        setError(data.error || t('An_error_occurred_while_setting_the_password'));
      }
    } catch (error) {
      console.error(error);
      setError(t('An_error_occurred_while_setting_the_password'));
    }
  };

  const handleUpdatePassword = async (
    oldPassword: string,
    newPassword: string,
    confirmNewPassword: string,
  ) => {
    if (newPassword !== confirmNewPassword) {
      setError(t('Passwords_do_not_match'));
      return;
    }

    if (oldPassword === "") {
      setError(t('Please_enter_your_old_password'));
      return;
    }

    if (newPassword === "") {
      setError(t('Please_enter_a_new_password'));
      return;
    }

    try {
      const res = await api.PUT("/auth/password-local", {
        oldPassword,
        newPassword,
      });

      if (res.ok) {
        setModalView("updateSuccess");
        // The rest of the app needs to revalidate the device authMode
        revalidator.revalidate();
      } else {
        const data = await res.json();
        setError(data.error || t('An_error_occurred_while_changing_the_password'));
      }
    } catch (error) {
      console.error(error);
      setError(t('An_error_occurred_while_changing_the_password'));
    }
  };

  const handleDeletePassword = async (password: string) => {
    if (password === "") {
      setError(t('Please_enter_your_current_password'));
      return;
    }

    try {
      const res = await api.DELETE("/auth/local-password", { password });
      if (res.ok) {
        setModalView("deleteSuccess");
        // The rest of the app needs to revalidate the device authMode
        revalidator.revalidate();
      } else {
        const data = await res.json();
        setError(data.error || t('An_error_occurred_while_disabling_the_password'));
      }
    } catch (error) {
      console.error(error);
      setError(t('An_error_occurred_while_disabling_the_password'));
    }
  };

  return (
    <div>
      <div>
        {modalView === "createPassword" && (
          <CreatePasswordModal
            onSetPassword={handleCreatePassword}
            onCancel={onClose}
            error={error}
          />
        )}

        {modalView === "deletePassword" && (
          <DeletePasswordModal
            onDeletePassword={handleDeletePassword}
            onCancel={onClose}
            error={error}
          />
        )}

        {modalView === "updatePassword" && (
          <UpdatePasswordModal
            onUpdatePassword={handleUpdatePassword}
            onCancel={onClose}
            error={error}
          />
        )}

        {modalView === "creationSuccess" && (
          <SuccessModal
            headline={t('Password_Set_Successfully')}
            description={t('You_ve_successfully_set_up_local_device_protection_Your_device_is_now_secure_against_unauthorized_local_access')}
            onClose={onClose}
          />
        )}

        {modalView === "deleteSuccess" && (
          <SuccessModal
            headline={t('Password_Protection_Disabled')}
            description={t('You_ve_successfully_disabled_the_password_protection_for_local_access_Remember_your_device_is_now_less_secure')}
            onClose={onClose}
          />
        )}

        {modalView === "updateSuccess" && (
          <SuccessModal
            headline={t('Password_Updated_Successfully')}
            description={t('You_ve_successfully_changed_your_local_device_protection_password_Make_sure_to_remember_your_new_password_for_future_access')}
            onClose={onClose}
          />
        )}
      </div>
    </div>
  );
}

function CreatePasswordModal({
  onSetPassword,
  onCancel,
  error,
}: {
  onSetPassword: (password: string, confirmPassword: string) => void;
  onCancel: () => void;
  error: string | null;
}) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");

  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <form
        className="space-y-4"
        onSubmit={e => {
          e.preventDefault();
        }}
      >
        <div>
          <h2 className="text-lg font-semibold dark:text-white">
              {t('Local_Device_Protection')}
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
              {t('Create_a_password_to_protect_your_device_from_unauthorized_local_access')}
          </p>
        </div>
        <InputFieldWithLabel
          label={t('New_Password')}
          type="password"
          placeholder={t('Enter_a_strong_password')}
          value={password}
          autoFocus
          onChange={e => setPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label={t('Confirm_New_Password')}
          type="password"
          placeholder={t('Re-enter_your_password')}
          value={confirmPassword}
          onChange={e => setConfirmPassword(e.target.value)}
        />

        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text={t('Secure_Device')}
            onClick={() => onSetPassword(password, confirmPassword)}
          />
          <Button size="SM" theme="light" text={t('Not_Now')} onClick={onCancel} />
        </div>
        {error && <p className="text-sm text-red-500">{error}</p>}
      </form>
    </div>
  );
}

function DeletePasswordModal({
  onDeletePassword,
  onCancel,
  error,
}: {
  onDeletePassword: (password: string) => void;
  onCancel: () => void;
  error: string | null;
}) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");

  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold dark:text-white">
              {t('Disable_Local_Device_Protection')}
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
              {t('Enter_your_current_password_to_disable_local_device_protection')}
          </p>
        </div>
        <InputFieldWithLabel
          label={t('Current_Password')}
          type="password"
          placeholder={t('Please_enter_your_current_password')}
          value={password}
          onChange={e => setPassword(e.target.value)}
        />
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="danger"
            text={t('Disable_Protection')}
            onClick={() => onDeletePassword(password)}
          />
          <Button size="SM" theme="light" text={t('Cancel')} onClick={onCancel} />
        </div>
        {error && <p className="text-sm text-red-500">{error}</p>}
      </div>
    </div>
  );
}

function UpdatePasswordModal({
  onUpdatePassword,
  onCancel,
  error,
}: {
  onUpdatePassword: (
    oldPassword: string,
    newPassword: string,
    confirmNewPassword: string,
  ) => void;
  onCancel: () => void;
  error: string | null;
}) {
  const { t } = useTranslation();
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmNewPassword, setConfirmNewPassword] = useState("");

  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <form
        className="space-y-4"
        onSubmit={e => {
          e.preventDefault();
        }}
      >
        <div>
          <h2 className="text-lg font-semibold dark:text-white">
              {t('Change_Local_Device_Password')}
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
              {t('Enter_your_current_password_and_a_new_password_to_update_your_local_device_protection')}
          </p>
        </div>
        <InputFieldWithLabel
          label={t('Current_Password')}
          type="password"
          placeholder={t('Please_enter_your_current_password')}
          value={oldPassword}
          onChange={e => setOldPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label={t('New_Password')}
          type="password"
          placeholder={t('Enter_a_new_strong_password')}
          value={newPassword}
          onChange={e => setNewPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label={t('Confirm_New_Password')}
          type="password"
          placeholder={t('Re-enter_your_new_password')}
          value={confirmNewPassword}
          onChange={e => setConfirmNewPassword(e.target.value)}
        />
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text={t('Update_Password')}
            onClick={() => onUpdatePassword(oldPassword, newPassword, confirmNewPassword)}
          />
          <Button size="SM" theme="light" text={t('Cancel')} onClick={onCancel} />
        </div>
        {error && <p className="text-sm text-red-500">{error}</p>}
      </form>
    </div>
  );
}

function SuccessModal({
  headline,
  description,
  onClose,
}: {
  headline: string;
  description: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex w-full max-w-lg flex-col items-start justify-start space-y-4 text-left">
      <div className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold dark:text-white">{headline}</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">{description}</p>
        </div>
        <Button size="SM" theme="primary" text={t('Close')} onClick={onClose} />
      </div>
    </div>
  );
}
