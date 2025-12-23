import { Form, redirect, useActionData, useLoaderData } from "react-router";
import type {
  ActionFunction,
  ActionFunctionArgs,
  LoaderFunction,
  LoaderFunctionArgs,
} from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";

import { User } from "@hooks/stores";
import { Button, LinkButton } from "@components/Button";
import Card from "@components/Card";
import { CardHeader } from "@components/CardHeader";
import DashboardNavbar from "@components/Header";
import Fieldset from "@components/Fieldset";
import { checkAuth } from "@/main";
import { CLOUD_API } from "@/ui.config";
import { m } from "@localizations/messages.js";

interface LoaderData {
  device: { id: string; name: string; user: { googleId: string } };
  user: User;
}

const action: ActionFunction = async ({ request }: ActionFunctionArgs) => {
  const { deviceId } = Object.fromEntries(await request.formData());

  try {
    const res = await fetch(`${CLOUD_API}/devices/${deviceId}`, {
      method: "DELETE",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      mode: "cors",
    });

    if (!res.ok) {
      return { message: m.deregister_error({ status: res.statusText }) };
    }
  } catch (e) {
    console.error(e);
    const message = e instanceof Error ? e.message : String(e);
    return { message: m.deregister_error({ status: message }) };
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
    return { user };
  }
};

export default function DevicesIdDeregister() {
  const { device, user } = useLoaderData() as LoaderData;
  const error = useActionData() as { message: string };

  return (
    <div className="grid min-h-screen grid-rows-(--grid-layout)">
      <DashboardNavbar
        isLoggedIn={!!user}
        primaryLinks={[{ title: m.deregister_cloud_devices(), to: "/devices" }]}
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
                <div className="max-w-xl space-y-4">
                  <CardHeader
                    headline={m.deregister_headline({ device: device.name || device.id })}
                    description={m.deregister_description()}
                  />

                  <Fieldset>
                    <Form method="POST" className="max-w-sm space-y-1.5">
                      <div className="flex gap-x-2">
                        <input name="deviceId" type="hidden" value={device.id} />
                        <LinkButton
                          size="MD"
                          theme="light"
                          to="/devices"
                          text={m.cancel()}
                          textAlign="center"
                        />
                        <Button
                          size="MD"
                          theme="danger"
                          type="submit"
                          text={m.deregister_from_cloud()}
                          textAlign="center"
                        />
                      </div>
                      {error?.message && (
                        <p className="text-sm text-red-500 dark:text-red-400">
                          {m.deregister_error({ status: error.message })}
                        </p>
                      )}
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

DevicesIdDeregister.loader = loader;
DevicesIdDeregister.action = action;
