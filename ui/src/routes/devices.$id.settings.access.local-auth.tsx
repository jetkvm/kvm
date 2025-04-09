import { useState, useEffect } from "react";
import { useLocation, useRevalidator } from "react-router-dom";

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
  const { modalView, setModalView } = useLocalAuthModalStore();
  const [error, setError] = useState<string | null>(null);
  const revalidator = useRevalidator();

  const handleCreatePassword = async (password: string, confirmPassword: string) => {
    if (password === "") {
      setError("Please enter a password");
      return;
    }

    if (password !== confirmPassword) {
      setError("Passwords do not match");
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
        setError(data.error || "An error occurred while setting the password");
      }
    } catch (error) {
      console.error(error);
      setError("An error occurred while setting the password");
    }
  };

  const handleUpdatePassword = async (
    oldPassword: string,
    newPassword: string,
    confirmNewPassword: string,
  ) => {
    if (newPassword !== confirmNewPassword) {
      setError("Passwords do not match");
      return;
    }

    if (oldPassword === "") {
      setError("Please enter your old password");
      return;
    }

    if (newPassword === "") {
      setError("Please enter a new password");
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
        setError(data.error || "An error occurred while changing the password");
      }
    } catch (error) {
      console.error(error);
      setError("An error occurred while changing the password");
    }
  };

  const handleDeletePassword = async (password: string) => {
    if (password === "") {
      setError("Please enter your current password");
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
        setError(data.error || "An error occurred while disabling the password");
      }
    } catch (error) {
      console.error(error);
      setError("An error occurred while disabling the password");
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
            headline="Password Set Successfully"
            description="You've successfully set up local device protection. Your device is now secure against unauthorized local access."
            onClose={onClose}
          />
        )}

        {modalView === "deleteSuccess" && (
          <SuccessModal
            headline="Password Protection Disabled"
            description="You've successfully disabled the password protection for local access. Remember, your device is now less secure."
            onClose={onClose}
          />
        )}

        {modalView === "updateSuccess" && (
          <SuccessModal
            headline="Password Updated Successfully"
            description="You've successfully changed your local device protection password. Make sure to remember your new password for future access."
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
          <h2 className="text-lg font-semibold dark:text-white">
            Local Device Protection
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Create a password to protect your device from unauthorized local access.
          </p>
        </div>
        <InputFieldWithLabel
          label="New Password"
          type="password"
          placeholder="Enter a strong password"
          value={password}
          autoFocus
          onChange={e => setPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label="Confirm New Password"
          type="password"
          placeholder="Re-enter your password"
          value={confirmPassword}
          onChange={e => setConfirmPassword(e.target.value)}
        />

        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text="Secure Device"
            onClick={() => onSetPassword(password, confirmPassword)}
          />
          <Button size="SM" theme="light" text="Not Now" onClick={onCancel} />
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
            Disable Local Device Protection
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Enter your current password to disable local device protection.
          </p>
        </div>
        <InputFieldWithLabel
          label="Current Password"
          type="password"
          placeholder="Enter your current password"
          value={password}
          onChange={e => setPassword(e.target.value)}
        />
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="danger"
            text="Disable Protection"
            onClick={() => onDeletePassword(password)}
          />
          <Button size="SM" theme="light" text="Cancel" onClick={onCancel} />
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
            Change Local Device Password
          </h2>
          <p className="text-sm text-slate-600 dark:text-slate-400">
            Enter your current password and a new password to update your local device
            protection.
          </p>
        </div>
        <InputFieldWithLabel
          label="Current Password"
          type="password"
          placeholder="Enter your current password"
          value={oldPassword}
          onChange={e => setOldPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label="New Password"
          type="password"
          placeholder="Enter a new strong password"
          value={newPassword}
          onChange={e => setNewPassword(e.target.value)}
        />
        <InputFieldWithLabel
          label="Confirm New Password"
          type="password"
          placeholder="Re-enter your new password"
          value={confirmNewPassword}
          onChange={e => setConfirmNewPassword(e.target.value)}
        />
        <div className="flex gap-x-2">
          <Button
            size="SM"
            theme="primary"
            text="Update Password"
            onClick={() => onUpdatePassword(oldPassword, newPassword, confirmNewPassword)}
          />
          <Button size="SM" theme="light" text="Cancel" onClick={onCancel} />
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
        <Button size="SM" theme="primary" text="Close" onClick={onClose} />
      </div>
    </div>
  );
}
