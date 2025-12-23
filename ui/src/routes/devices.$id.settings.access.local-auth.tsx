import { useState, useEffect } from "react";
import { useLocation, useRevalidator } from "react-router";

import { useLocalAuthModalStore } from "@hooks/stores";
import { useDeviceUiNavigation } from "@hooks/useAppNavigation";
import { Button } from "@components/Button";
import { InputFieldWithLabel } from "@/components/InputField";
import api from "@/api";
import { m } from "@localizations/messages.js";

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

  return <Dialog onClose={() => navigateTo("..")} />;
}

export function Dialog({ onClose }: Readonly<{ onClose: () => void }>) {
  const { modalView, setModalView } = useLocalAuthModalStore();
  const [error, setError] = useState<string | null>(null);
  const revalidator = useRevalidator();

  const handleCreatePassword = async (password: string, confirmPassword: string) => {
    if (password === "") {
      setError(m.local_auth_error_enter_password());
      return;
    }

    if (password !== confirmPassword) {
      setError(m.local_auth_error_passwords_not_match());
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
        setError(data.error || m.local_auth_error_setting_password());
      }
    } catch (error) {
      console.error(error);
      setError(m.local_auth_error_setting_password());
    }
  };

  const handleUpdatePassword = async (
    oldPassword: string,
    newPassword: string,
    confirmNewPassword: string,
  ) => {
    if (newPassword !== confirmNewPassword) {
      setError(m.local_auth_error_passwords_not_match());
      return;
    }

    if (oldPassword === "") {
      setError(m.local_auth_error_enter_old_password());
      return;
    }

    if (newPassword === "") {
      setError(m.local_auth_error_enter_new_password());
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
        setError(data.error || m.local_auth_error_changing_password());
      }
    } catch (error) {
      console.error(error);
      setError(m.local_auth_error_changing_password());
    }
  };

  const handleDeletePassword = async (password: string) => {
    if (password === "") {
      setError(m.local_auth_error_enter_current_password());
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
        setError(data.error || m.local_auth_error_disabling_password());
      }
    } catch (error) {
      console.error(error);
      setError(m.local_auth_error_disabling_password());
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
            headline={m.local_auth_success_password_set_title()}
            description={m.local_auth_success_password_set_description()}
            onClose={onClose}
          />
        )}

        {modalView === "deleteSuccess" && (
          <SuccessModal
            headline={m.local_auth_success_password_disabled_title()}
            description={m.local_auth_success_password_disabled_description()}
            onClose={onClose}
          />
        )}

        {modalView === "updateSuccess" && (
          <SuccessModal
            headline={m.local_auth_success_password_updated_title()}
            description={m.local_auth_success_password_updated_description()}
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
          <h2 className="text-lg font-semibold dark:text-white">{m.local_auth_create_title()}</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {m.local_auth_create_description()}
          </p>
        </div>
        <InputFieldWithLabel
          label={m.local_auth_create_new_password_label()}
          type="password"
          placeholder={m.local_auth_create_new_password_placeholder()}
          value={password}
          autoFocus
          onChange={e => setPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label={m.local_auth_confirm_new_password_label()}
          type="password"
          placeholder={m.local_auth_create_confirm_password_placeholder()}
          value={confirmPassword}
          onChange={e => setConfirmPassword(e.target.value)}
        />

        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text={m.local_auth_create_secure_button()}
            onClick={() => onSetPassword(password, confirmPassword)}
          />
          <Button
            size="SM"
            theme="light"
            text={m.local_auth_create_not_now_button()}
            onClick={onCancel}
          />
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
  const [password, setPassword] = useState("");

  return (
    <div className="flex flex-col items-start justify-start space-y-4 text-left">
      <div className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold dark:text-white">
            {m.local_auth_disable_local_device_protection_title()}
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {m.local_auth_disable_local_device_protection_description()}
          </p>
        </div>
        <InputFieldWithLabel
          label={m.local_auth_current_password_label()}
          type="password"
          placeholder={m.local_auth_enter_current_password_placeholder()}
          value={password}
          onChange={e => setPassword(e.target.value)}
        />
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="danger"
            text={m.local_auth_disable_protection_button()}
            onClick={() => onDeletePassword(password)}
          />
          <Button size="SM" theme="light" text={m.cancel()} onClick={onCancel} />
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
  onUpdatePassword: (oldPassword: string, newPassword: string, confirmNewPassword: string) => void;
  onCancel: () => void;
  error: string | null;
}) {
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
            {m.local_auth_change_local_device_password_title()}
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            {m.local_auth_change_local_device_password_description()}
          </p>
        </div>
        <InputFieldWithLabel
          label={m.local_auth_current_password_label()}
          type="password"
          placeholder={m.local_auth_enter_current_password_placeholder()}
          value={oldPassword}
          onChange={e => setOldPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label={m.local_auth_new_password_label()}
          type="password"
          placeholder={m.local_auth_enter_new_password_placeholder()}
          value={newPassword}
          onChange={e => setNewPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label={m.local_auth_confirm_new_password_label()}
          type="password"
          placeholder={m.local_auth_reenter_new_password_placeholder()}
          value={confirmNewPassword}
          onChange={e => setConfirmNewPassword(e.target.value)}
        />
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text={m.local_auth_update_password_button()}
            onClick={() => onUpdatePassword(oldPassword, newPassword, confirmNewPassword)}
          />
          <Button size="SM" theme="light" text={m.cancel()} onClick={onCancel} />
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
  return (
    <div className="flex w-full max-w-lg flex-col items-start justify-start space-y-4 text-left">
      <div className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold dark:text-white">{headline}</h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">{description}</p>
        </div>
        <Button size="SM" theme="primary" text={m.close()} onClick={onClose} />
      </div>
    </div>
  );
}
