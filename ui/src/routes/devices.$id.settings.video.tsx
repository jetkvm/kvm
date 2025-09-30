import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/Button";
import { TextAreaWithLabel } from "@/components/TextArea";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import { SettingsPageHeader } from "@components/SettingsPageheader";
import { useSettingsStore } from "@/hooks/stores";
import { SelectMenuBasic } from "@components/SelectMenuBasic";
import Fieldset from "@components/Fieldset";
import notifications from "@/notifications";

import { SettingsItem } from "./devices.$id.settings";

export default function SettingsVideoRoute() {
  const { send } = useJsonRpc();
  const { t } = useTranslation();
  const [streamQuality, setStreamQuality] = useState("1");
  const [customEdidValue, setCustomEdidValue] = useState<string | null>(null);
  const [edid, setEdid] = useState<string | null>(null);
  const [edidLoading, setEdidLoading] = useState(false);

  const defaultEdid =
    "00ffffffffffff0052620188008888881c150103800000780a0dc9a05747982712484c00000001010101010101010101010101010101023a801871382d40582c4500c48e2100001e011d007251d01e206e285500c48e2100001e000000fc00543734392d6648443732300a20000000fd00147801ff1d000a202020202020017b";
  const edids = [
    {
      value: defaultEdid,
      label: t('JetKVM_Default'),
    },
    {
      value:
        "00FFFFFFFFFFFF00047265058A3F6101101E0104A53420783FC125A8554EA0260D5054BFEF80714F8140818081C081008B009500B300283C80A070B023403020360006442100001A000000FD00304C575716010A202020202020000000FC0042323436574C0A202020202020000000FF0054384E4545303033383532320A01F802031CF14F90020304050607011112131415161F2309070783010000011D8018711C1620582C250006442100009E011D007251D01E206E28550006442100001E8C0AD08A20E02D10103E9600064421000018C344806E70B028401720A80406442100001E00000000000000000000000000000000000000000000000000000096",
      label: "Acer B246WL, 1920x1200",
    },
    {
      value:
        "00FFFFFFFFFFFF0006B3872401010101021F010380342078EA6DB5A7564EA0250D5054BF6F00714F8180814081C0A9409500B300D1C0283C80A070B023403020360006442100001A000000FD00314B1E5F19000A202020202020000000FC00504132343851560A2020202020000000FF004D314C4D51533035323135370A014D02032AF14B900504030201111213141F230907078301000065030C001000681A00000101314BE6E2006A023A801871382D40582C450006442100001ECD5F80B072B0374088D0360006442100001C011D007251D01E206E28550006442100001E8C0AD08A20E02D10103E960006442100001800000000000000000000000000DC",
      label: "ASUS PA248QV, 1920x1200",
    },
    {
      value:
        "00FFFFFFFFFFFF0010AC132045393639201E0103803C22782ACD25A3574B9F270D5054A54B00714F8180A9C0D1C00101010101010101023A801871382D40582C450056502100001E000000FF00335335475132330A2020202020000000FC0044454C4C204432373231480A20000000FD00384C1E5311000A202020202020018102031AB14F90050403020716010611121513141F65030C001000023A801871382D40582C450056502100001E011D8018711C1620582C250056502100009E011D007251D01E206E28550056502100001E8C0AD08A20E02D10103E960056502100001800000000000000000000000000000000000000000000000000000000004F",
      label: "DELL D2721H, 1920x1080",
    },
    {
      value:
        "00ffffffffffff0010ac0100020000000111010380221bff0a00000000000000000000adce0781800101010101010101010101010101000000ff0030303030303030303030303030000000ff0030303030303030303030303030000000fd00384c1f530b000a000000000000000000fc0044454c4c2049445241430a2020000a",
      label: "DELL IDRAC EDID, 1280x1024",
    },
  ];

  const streamQualityOptions = [
    { value: "1", label: t("High") },
    { value: "0.5", label: t('Medium') },
    { value: "0.1", label: t('Low') },
  ];
  // Video enhancement settings from store
  const {
    videoSaturation,
    setVideoSaturation,
    videoBrightness,
    setVideoBrightness,
    videoContrast,
    setVideoContrast,
  } = useSettingsStore();

  useEffect(() => {
    setEdidLoading(true);
    send("getStreamQualityFactor", {}, (resp: JsonRpcResponse) => {
      if ("error" in resp) return;
      setStreamQuality(String(resp.result));
    });

    send("getEDID", {}, (resp: JsonRpcResponse) => {
      setEdidLoading(false);
      if ("error" in resp) {
        notifications.error(t('Failed_to_get_EDID_msg',{msg:resp.error.data || t('Unknown_error')}))
        return;
      }

      const receivedEdid = resp.result as string;

      const matchingEdid = edids.find(
        x => x.value.toLowerCase() === receivedEdid.toLowerCase(),
      );

      if (matchingEdid) {
        // EDID is stored in uppercase in the UI
        setEdid(matchingEdid.value.toUpperCase());
        // Reset custom EDID value
        setCustomEdidValue(null);
      } else {
        setEdid("custom");
        setCustomEdidValue(receivedEdid);
      }
    });
  }, [send]);

  const handleStreamQualityChange = (factor: string) => {
    send(
      "setStreamQualityFactor",
      { factor: Number(factor) },
      (resp: JsonRpcResponse) => {
        if ("error" in resp) {
          notifications.error(
            t('Failed_to_set_stream_quality_msg',{msg:resp.error.data || t('Unknown_error')})
          );
          return;
        }

        notifications.success(
          t('Stream_quality_set_to_msg',{msg:streamQualityOptions.find(x => x.value === factor)?.label})
        );
        setStreamQuality(factor);
      },
    );
  };

  const handleEDIDChange = (newEdid: string) => {
    setEdidLoading(true);
    send("setEDID", { edid: newEdid }, (resp: JsonRpcResponse) => {
      setEdidLoading(false);
      if ("error" in resp) {
        notifications.error(t('Failed_to_set_EDID_msg',{msg:resp.error.data || t('Unknown_error')}))
        return;
      }

      notifications.success(
        t('EDID_set_successfully_to_msg',{msg:edids.find(x => x.value === newEdid)?.label ?? t('the_custom_EDID')})
      );
      // Update the EDID value in the UI
      setEdid(newEdid);
    });
  };

  return (
    <div className="space-y-3">
      <div className="space-y-4">
        <SettingsPageHeader
          title={t('Video')}
          description={t('Configure_display_settings_and_EDID_for_optimal_compatibility')}
        />

        <div className="space-y-4">
          <div className="space-y-4">
            <SettingsItem
              title={t('Stream_Quality')}
              description={t('Adjust_the_quality_of_the_video_stream')}
            >
              <SelectMenuBasic
                size="SM"
                label=""
                value={streamQuality}
                options={streamQualityOptions}
                onChange={e => handleStreamQualityChange(e.target.value)}
              />
            </SettingsItem>

            {/* Video Enhancement Settings */}
            <SettingsItem
              title={t('Video_Enhancement')}
              description={t('Adjust_color_settings_to_make_the_video_output_more_vibrant_and_colorful')}
            />

            <div className="space-y-4 pl-4">
              <SettingsItem
                title={t('Saturation')}
                description={t('Color_saturation_sat_x',{sat:videoSaturation.toFixed(1)})}
              >
                <input
                  type="range"
                  min="0.5"
                  max="2.0"
                  step="0.1"
                  value={videoSaturation}
                  onChange={e => setVideoSaturation(parseFloat(e.target.value))}
                  className="h-2 w-32 cursor-pointer appearance-none rounded-lg bg-gray-200 dark:bg-gray-700"
                />
              </SettingsItem>

              <SettingsItem
                title={t('Brightness')}
                description={t('Brightness_level_brightness',{brightness:videoBrightness.toFixed(1)})}
              >
                <input
                  type="range"
                  min="0.5"
                  max="1.5"
                  step="0.1"
                  value={videoBrightness}
                  onChange={e => setVideoBrightness(parseFloat(e.target.value))}
                  className="h-2 w-32 cursor-pointer appearance-none rounded-lg bg-gray-200 dark:bg-gray-700"
                />
              </SettingsItem>

              <SettingsItem
                title={t('Contrast')}
                description={t('Contrast_level_contrast',{contrast:videoContrast.toFixed(1)})}
              >
                <input
                  type="range"
                  min="0.5"
                  max="2.0"
                  step="0.1"
                  value={videoContrast}
                  onChange={e => setVideoContrast(parseFloat(e.target.value))}
                  className="h-2 w-32 cursor-pointer appearance-none rounded-lg bg-gray-200 dark:bg-gray-700"
                />
              </SettingsItem>

              <div className="flex gap-2">
                <Button
                  size="SM"
                  theme="light"
                  text={t('Reset_to_Default')}
                  onClick={() => {
                    setVideoSaturation(1.0);
                    setVideoBrightness(1.0);
                    setVideoContrast(1.0);
                  }}
                />
              </div>
            </div>
            <Fieldset disabled={edidLoading} className="space-y-2">
              <SettingsItem
                title="EDID"
                description={t('Adjust_the_EDID_settings_for_the_display')}
                loading={edidLoading}
              >
                <SelectMenuBasic
                  size="SM"
                  label=""
                  fullWidth
                  value={customEdidValue ? "custom" : edid || "asd"}
                  onChange={e => {
                    if (e.target.value === "custom") {
                      setEdid("custom");
                      setCustomEdidValue("");
                    } else {
                      setCustomEdidValue(null);
                      handleEDIDChange(e.target.value as string);
                    }
                  }}
                  options={[...edids, { value: "custom", label: t('Custom') }]}
                />
              </SettingsItem>
              {customEdidValue !== null && (
                <>
                  <SettingsItem
                    title={t('Custom_EDID')}
                    description={t('EDID_details_video_mode_compatibility_Default_settings_works_in_most_cases')}
                  />
                  <TextAreaWithLabel
                    label={t('EDID_File')}
                    placeholder="00F..."
                    rows={3}
                    value={customEdidValue}
                    onChange={e => setCustomEdidValue(e.target.value)}
                  />
                  <div className="flex justify-start gap-x-2">
                    <Button
                      size="SM"
                      theme="primary"
                      text={t('Set_Custom_EDID')}
                      loading={edidLoading}
                      onClick={() => handleEDIDChange(customEdidValue)}
                    />
                    <Button
                      size="SM"
                      theme="light"
                      text={t('Restore_to_Default')}
                      loading={edidLoading}
                      onClick={() => {
                        setCustomEdidValue(null);
                        handleEDIDChange(defaultEdid);
                      }}
                    />
                  </div>
                </>
              )}
            </Fieldset>
          </div>
        </div>
      </div>
    </div>
  );
}