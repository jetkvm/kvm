import { Form, redirect, useActionData, useLoaderData } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction, LoaderFunctionArgs } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import { useTranslation } from "react-i18next";


import { Button, LinkButton } from "@components/Button";
import Card from "@components/Card";
import { CardHeader } from "@components/CardHeader";
import { InputFieldWithLabel } from "@components/InputField";
import DashboardNavbar from "@components/Header";
import { User } from "@/hooks/stores";
import { checkAuth } from "@/main";
import Fieldset from "@components/Fieldset";
import { CLOUD_API } from "@/ui.config";

import api from "../api";

interface LoaderData {
  device: { id: string; name: string; user: { googleId: string } };
  user: User;
}
// eslint-disable-next-line react-hooks/rules-of-hooks
const action: ActionFunction = async ({ params, request }: ActionFunctionArgs) => {
  const { id } = params;
  const { name } = Object.fromEntries(await request.formData());
  const { t } = useTranslation();
  if (!name || name === "") {
    return { message: t('Please_specify_a_name') };
  }

  try {
    const res = await api.PUT(`${CLOUD_API}/devices/${id}`, {
      name,
    });
    if (!res.ok) {
      return { message: t('There_was_an_error_renaming_your_device_Please_try_again') };
    }
  } catch (e) {
    console.error(e);
    return { message: t('There_was_an_error_renaming_your_device_Please_try_again') };
  }

  return redirect("/devices");
};

const loader: LoaderFunction = async ({ params }: LoaderFunctionArgs) => {
  const user = await checkAuth();
  const { id } = params;

  try {
    const res = await fetch(`${CLOUD_API}/devices/${id}`, {
      method: "GET",
      credentials: "include",
      mode: "cors",
    });

    const { device } = (await res.json()) as {
      device: { id: string; name: string; user: { googleId: string } };
    };

    return { device, user };
  } catch (e) {
    console.error(e);
    return { devices: [] };
  }
};

export default function DeviceIdRename() {
  const { device, user } = useLoaderData() as LoaderData;
  const error = useActionData() as { message: string };
  const { t } = useTranslation();
  return (
    <div className="grid min-h-screen grid-rows-(--grid-layout)">
      <DashboardNavbar
        isLoggedIn={!!user}
        primaryLinks={[{ title: "Cloud Devices", to: "/devices" }]}
        userEmail={user?.email}
        picture={user?.picture}
        kvmName={device?.name}
      />

      <div className="h-full w-full">
        <div className="mt-4">
          <div className="mx-auto h-full w-full space-y-6 px-4 sm:max-w-6xl sm:px-8 md:max-w-7xl md:px-12 lg:max-w-8xl">
            <div className="space-y-4">
              <LinkButton
                size="SM"
                theme="blank"
                LeadingIcon={ChevronLeftIcon}
                text={t('Back_to_Devices')}
                to="/devices"
              />
              <Card className="max-w-3xl p-6">
                <div className="space-y-4">
                  <CardHeader
                    headline={`Rename ${device.name || device.id}`}
                    description={t('Properly_name_your_device_to_easily_identify_it')}
                  />

                  <Fieldset>
                    <Form method="POST" className="max-w-sm space-y-4">
                      <div className="group relative">
                        <InputFieldWithLabel
                          label={t('New_device_name')}
                          type="text"
                          name="name"
                          placeholder={t('Plex_Media_Server')}
                          size="MD"
                          autoFocus
                          error={error?.message.toString()}
                        />
                      </div>

                      <Button
                        size="MD"
                        theme="primary"
                        type="submit"
                        text={t('Rename_Device')}
                        textAlign="center"
                      />
                    </Form>
                  </Fieldset>
                </div>
              </Card>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

DeviceIdRename.loader = loader;
DeviceIdRename.action = action;
