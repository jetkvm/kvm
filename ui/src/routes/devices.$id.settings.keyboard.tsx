import { useCallback, useEffect, useMemo, useState } from "react";
import { LuTrash2, LuUpload, LuRefreshCw, LuEye, LuCheck, LuChevronsUpDown } from "react-icons/lu";
import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from "@headlessui/react";

import { cx } from "@/cva.config";
import { useSettingsStore } from "@hooks/stores";
import { JsonRpcResponse, useJsonRpc } from "@hooks/useJsonRpc";
import { Button } from "@components/Button";
import { Checkbox } from "@components/Checkbox";
import { ConfirmDialog } from "@components/ConfirmDialog";
import { SettingsItem } from "@components/SettingsItem";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useKleUpload } from "@components/keyboard/useKleUpload";
import { LayoutPreviewDialog } from "@components/keyboard/LayoutPreviewDialog";
import type { LayoutMeta } from "@components/keyboard/types/schema";
import notifications from "@/notifications";
import { m } from "@localizations/messages.js";

const FALLBACK_LAYOUT = "en-US";

export default function SettingsKeyboardRoute() {
  const { keyboardLayout, setKeyboardLayout } = useSettingsStore();
  const { showPressedKeys, setShowPressedKeys } = useSettingsStore();
  const { modifierLatching, setModifierLatching } = useSettingsStore();
  const { send } = useJsonRpc();

  const [layouts, setLayouts] = useState<LayoutMeta[]>([]);
  const [deleteTarget, setDeleteTarget] = useState<LayoutMeta | null>(null);
  const [previewLayoutId, setPreviewLayoutId] = useState<string | null>(null);

  const { result: uploadResult, error: uploadError, openFilePicker, clear: clearUpload } =
    useKleUpload();

  const refreshLayouts = useCallback(() => {
    void send("getKeyboardLayouts", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setLayouts(resp.result as LayoutMeta[]);
    });
  }, [send]);

  // Fetch the active layout ID from config and the available layouts list
  useEffect(() => {
    void send("getKeyboardLayout", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      const id = resp.result as string;
      if (id && id.length > 0) {
        setKeyboardLayout(id);
      }
    });
    refreshLayouts();
  }, [send, setKeyboardLayout, refreshLayouts]);

  // Handle upload result — open preview dialog
  useEffect(() => {
    if (uploadResult) {
      notifications.success(
        m.keyboard_layout_custom_upload_success({
          name: uploadResult.name,
          keys: uploadResult.keyCount,
        }),
      );
      if (uploadResult.warnings?.length) {
        for (const warning of uploadResult.warnings) {
          notifications.error(warning, { duration: 8000 });
        }
      }
      setPreviewLayoutId(uploadResult.id);
      clearUpload();
      refreshLayouts();
    }
    if (uploadError) {
      notifications.error(m.keyboard_layout_custom_upload_error({ error: uploadError }));
      clearUpload();
    }
  }, [uploadResult, uploadError, clearUpload, refreshLayouts]);

  const customLayouts = useMemo(() => layouts.filter(l => !l.builtin), [layouts]);

  const onKeyboardLayoutChange = useCallback(
    (id: string) => {
      void send("setKeyboardLayout", { layout: id }, resp => {
        if ("error" in resp) {
          notifications.error(
            m.keyboard_layout_error({ error: resp.error.data || m.unknown_error() }),
          );
          return;
        }
        const layoutName = layouts.find(l => l.id === id)?.name ?? id;
        notifications.success(m.keyboard_layout_success({ layout: layoutName }));
        setKeyboardLayout(id);
      });
    },
    [send, setKeyboardLayout, layouts],
  );

  const handleDeleteLayout = useCallback(() => {
    if (!deleteTarget) return;
    const { id, name } = deleteTarget;

    void send("deleteKeyboardLayout", { id }, resp => {
      if ("error" in resp) {
        notifications.error(
          m.keyboard_layout_delete_error({ error: resp.error.data || m.unknown_error() }),
        );
        setDeleteTarget(null);
        return;
      }

      notifications.success(m.keyboard_layout_delete_success({ name }));
      setDeleteTarget(null);
      refreshLayouts();

      // If the deleted layout was the active one, switch to fallback
      if (keyboardLayout === id) {
        void send("setKeyboardLayout", { layout: FALLBACK_LAYOUT }, () => {
          setKeyboardLayout(FALLBACK_LAYOUT);
        });
      }
    });
  }, [deleteTarget, send, refreshLayouts, keyboardLayout, setKeyboardLayout]);

  return (
    <div className="space-y-4">
      <SettingsPageHeader title={m.keyboard_title()} description={m.keyboard_description()} />

      <div className="space-y-4">
        <SettingsItem
          title={m.keyboard_layout_title()}
          description={m.keyboard_layout_description()}
        >
          <Listbox value={keyboardLayout} onChange={onKeyboardLayoutChange}>
            <div className="relative w-full">
              <ListboxButton
                className={cx(
                  "relative w-full cursor-pointer rounded-md py-2 pr-10 pl-3 text-left text-[13px]",
                  "border border-slate-300 bg-white dark:border-slate-600 dark:bg-slate-800",
                  "text-slate-900 dark:text-slate-100",
                )}
              >
                <span className="block truncate">
                  {layouts.find(l => l.id === keyboardLayout)?.name ?? keyboardLayout}
                </span>
                <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2">
                  <LuChevronsUpDown className="h-4 w-4 text-slate-400" />
                </span>
              </ListboxButton>
              <ListboxOptions
                anchor="bottom start"
                transition
                className={cx(
                  "z-30 mt-1 max-h-60 w-[var(--button-width)] overflow-auto rounded-md py-1",
                  "border border-slate-200 bg-white shadow-lg dark:border-slate-700 dark:bg-slate-800",
                  "transition duration-100 ease-out data-closed:opacity-0",
                )}
              >
                {layouts.map(layout => (
                  <ListboxOption
                    key={layout.id}
                    value={layout.id}
                    className={cx(
                      "group relative flex cursor-pointer items-center py-2 pr-2 pl-9 text-[13px]",
                      "text-slate-900 dark:text-slate-100",
                      "data-focus:bg-blue-50 dark:data-focus:bg-slate-700",
                    )}
                  >
                    <span className="absolute inset-y-0 left-0 hidden items-center pl-2.5 group-data-selected:flex">
                      <LuCheck className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                    </span>
                    <span className="flex-1 truncate">{layout.name}</span>
                    <button
                      type="button"
                      className={cx(
                        "ml-2 rounded p-1 transition-opacity",
                        "text-slate-300 hover:text-blue-500 dark:text-slate-500 dark:hover:text-blue-400",
                        "group-data-focus:text-slate-500 dark:group-data-focus:text-slate-300",
                      )}
                      data-testid={`preview-layout-${layout.id}`}
                      title={m.keyboard_layout_custom_preview()}
                      onClick={e => {
                        e.stopPropagation();
                        setPreviewLayoutId(layout.id);
                      }}
                    >
                      <LuEye className="h-3.5 w-3.5" />
                    </button>
                  </ListboxOption>
                ))}
              </ListboxOptions>
            </div>
          </Listbox>
        </SettingsItem>
        <p className="text-xs text-slate-600 dark:text-slate-400">
          {m.keyboard_layout_long_description()}
        </p>
      </div>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <SettingsItem
            title={m.keyboard_layout_custom_title()}
            description={m.keyboard_layout_custom_description()}
          />
          <Button
            size="XS"
            theme="light"
            text={m.keyboard_layout_custom_upload()}
            LeadingIcon={LuUpload}
            data-testid="upload-keyboard-layout"
            onClick={() => openFilePicker()}
          />
        </div>
        {customLayouts.length > 0 ? (
          <div className="space-y-2">
            {customLayouts.map(layout => (
              <div
                key={layout.id}
                className="flex items-center justify-between rounded-md border border-slate-200 px-3 py-2 dark:border-slate-700"
              >
                <span className="text-sm text-slate-700 dark:text-slate-300">
                  {layout.name}
                  <span className="ml-2 text-xs text-slate-400 dark:text-slate-500">
                    {layout.id}
                  </span>
                </span>
                <div className="flex items-center gap-1">
                  <Button
                    size="XS"
                    theme="light"
                    LeadingIcon={LuEye}
                    text={m.keyboard_layout_custom_preview()}
                    data-testid={`preview-layout-${layout.id}`}
                    onClick={() => setPreviewLayoutId(layout.id)}
                  />
                  <Button
                    size="XS"
                    theme="light"
                    LeadingIcon={LuRefreshCw}
                    text={m.keyboard_layout_custom_replace()}
                    data-testid={`replace-layout-${layout.id}`}
                    onClick={() => openFilePicker(layout.id)}
                  />
                  <Button
                    size="XS"
                    theme="light"
                    className="text-red-500 dark:text-red-400"
                    LeadingIcon={LuTrash2}
                    text={m.delete()}
                    data-testid={`delete-layout-${layout.id}`}
                    onClick={() => setDeleteTarget(layout)}
                  />
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="text-xs text-slate-500 dark:text-slate-400">
            {m.keyboard_layout_none_custom()}
          </p>
        )}
      </div>

      <div className="space-y-4">
        <SettingsItem
          title={m.keyboard_modifier_latching_title()}
          description={m.keyboard_modifier_latching_description()}
        >
          <Checkbox
            checked={modifierLatching}
            onChange={e => setModifierLatching(e.target.checked)}
          />
        </SettingsItem>
        <SettingsItem
          title={m.keyboard_show_pressed_keys_title()}
          description={m.keyboard_show_pressed_keys_description()}
        >
          <Checkbox
            checked={showPressedKeys}
            onChange={e => setShowPressedKeys(e.target.checked)}
          />
        </SettingsItem>
      </div>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title={m.keyboard_layout_delete_confirm_title()}
        description={m.keyboard_layout_delete_confirm_description({
          name: deleteTarget?.name ?? "",
        })}
        variant="danger"
        confirmText={m.delete()}
        onConfirm={handleDeleteLayout}
      />

      <LayoutPreviewDialog
        layoutId={previewLayoutId}
        onClose={() => setPreviewLayoutId(null)}
      />
    </div>
  );
}
