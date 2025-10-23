import { Form, redirect, useActionData, useLoaderData } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction, LoaderFunctionArgs } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";

import { Button, LinkButton } from "@components/Button";
import Card from "@components/Card";
import { CardHeader } from "@components/CardHeader";
import DashboardNavbar from "@components/Header";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel } from "@components/InputField";
import { checkAuth } from "@/main";
import { CLOUD_API } from "@/ui.config";
import api from "@/api";
import { m } from "@localizations/messages";

const action: ActionFunction = async ({ params, request }: ActionFunctionArgs) => {
  const { id } = params;
  const { name } = Object.fromEntries(await request.formData());

  if (!name || name === "") {
    return { message: m.rename_device_no_name() };
  }

  try {
    const res = await api.PUT(`${CLOUD_API}/devices/${id}`, {
      name,
    });
    if (!res.ok) {
      return { message: m.rename_device_error({ error: res.statusText }) };
    }
  } catch (e) {
    console.error(e);
    return { message: m.rename_device_error({ error: String(e) }) };
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
  const { device, user } = useLoaderData();
  const error = useActionData() as { message: string };

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
                text={m.back_to_devices()}
                to="/devices"
              />
              <Card className="max-w-3xl p-6">
                <div className="space-y-4">
                  <CardHeader
                    headline={m.rename_device_headline({ name: device.name || device.id })}
                    description={m.rename_device_description()}
                  />

                  <Fieldset>
                    <Form method="POST" className="max-w-sm space-y-4">
                      <div className="group relative">
                        <InputFieldWithLabel
                          label={m.rename_device_new_name_label()}
                          type="text"
                          name="name"
                          placeholder={m.rename_device_new_name_placeholder()}
                          size="MD"
                          autoFocus
                          error={error?.message.toString()}
                        />
                      </div>

                      <Button
                        size="MD"
                        theme="primary"
                        type="submit"
                        text={m.rename_device()}
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
