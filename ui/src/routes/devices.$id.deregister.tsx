import { Form, redirect, useActionData, useLoaderData } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction, LoaderFunctionArgs } from "react-router";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import { useTranslation } from "react-i18next";

import { Button, LinkButton } from "@components/Button";
import Card from "@components/Card";
import { CardHeader } from "@components/CardHeader";
import DashboardNavbar from "@components/Header";
import { User } from "@/hooks/stores";
import { checkAuth } from "@/main";
import Fieldset from "@components/Fieldset";
import { CLOUD_API } from "@/ui.config";

interface LoaderData {
  device: { id: string; name: string; user: { googleId: string } };
  user: User;
}
// eslint-disable-next-line react-hooks/rules-of-hooks
const action: ActionFunction = async ({ request }: ActionFunctionArgs) => {
  const { deviceId } = Object.fromEntries(await request.formData());
  const { t } = useTranslation();
  try {
    const res = await fetch(`${CLOUD_API}/devices/${deviceId}`, {
      method: "DELETE",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      mode: "cors",
    });

    if (!res.ok) {
      return { message: t('There_was_an_error_deregistering_your_device_Please_try_again') };
    }
  } catch (e) {
    console.error(e);
    return { message: t('There_was_an_error_deregistering_your_device_Please_try_again') };
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

export default function DevicesIdDeregister() {
  const { device, user } = useLoaderData() as LoaderData;
  const error = useActionData() as { message: string };
  const { t } = useTranslation();
  return (
    <div className="grid min-h-screen grid-rows-(--grid-layout)">
      <DashboardNavbar
        isLoggedIn={!!user}
        primaryLinks={[{ title: t('Cloud_Devices'), to: "/devices" }]}
        userEmail={user?.email}
        picture={user?.picture}
        kvmName={device?.name}
      />

      <div className="w-full h-full">
        <div className="mt-4">
          <div className="w-full h-full px-4 mx-auto space-y-6 sm:max-w-6xl sm:px-8 md:max-w-7xl md:px-12 lg:max-w-8xl">
            <div className="space-y-4">
              <LinkButton
                size="SM"
                theme="blank"
                LeadingIcon={ChevronLeftIcon}
                text={t('Back_to_Devices')}
                to="/devices"
              />
              <Card className="max-w-3xl p-6">
                <div className="max-w-xl space-y-4">
                  <CardHeader
                    headline={`Deregister ${device.name || device.id} from your cloud account`}
                    description={
                      <>
                          {t('This_will_remove_the_device_from_your_cloud_account_and_revoke_remote_access_to_it')}
                        <br />
                          {t('Please_note_that_local_access_will_still_be_possible')})
                      </>
                    }
                  />

                  <Fieldset>
                    <Form method="POST" className="max-w-sm space-y-1.5">
                      <div className="flex gap-x-2">
                        <input name="deviceId" type="hidden" value={device.id} />
                        <LinkButton
                          size="MD"
                          theme="light"
                          to="/devices"
                          text={t('Cancel')}
                          textAlign="center"
                        />
                        <Button
                          size="MD"
                          theme="danger"
                          type="submit"
                          text={t('Deregister_from_Cloud')}
                          textAlign="center"
                        />
                      </div>
                      {error?.message && (
                        <p className="text-sm text-red-500 dark:text-red-400">
                          {error?.message}
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
