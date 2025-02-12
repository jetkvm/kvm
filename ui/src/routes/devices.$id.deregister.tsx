import {
  ActionFunctionArgs,
  Form,
  LoaderFunctionArgs,
  redirect,
  useActionData,
  useLoaderData,
} from "react-router-dom";
import { Button, LinkButton } from "@components/Button";
import Card from "@components/Card";
import { CardHeader } from "@components/CardHeader";
import DashboardNavbar from "@components/Header";
import { User } from "@/hooks/stores";
import { checkAuth } from "@/main";
import Fieldset from "@components/Fieldset";
import { ChevronLeftIcon } from "@heroicons/react/16/solid";
import { CLOUD_API } from "@/ui.config";

interface LoaderData {
  device: { id: string; name: string; user: { googleId: string } };
  user: User;
}

const action = async ({ request }: ActionFunctionArgs) => {
  const { deviceId } = Object.fromEntries(await request.formData());

  try {
    const res = await fetch(`${CLOUD_API}/devices/${deviceId}`, {
      method: "DELETE",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      mode: "cors",
    });

    if (!res.ok) {
      return { message: "There was an error renaming your device. Please try again." };
    }
  } catch (e) {
    return { message: "There was an error renaming your device. Please try again." };
  }

  return redirect("/devices");
};

const loader = async ({ params }: LoaderFunctionArgs) => {
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

  return (
    <div className="grid min-h-screen grid-rows-layout">
      <DashboardNavbar
        isLoggedIn={!!user}
        primaryLinks={[{ title: "Cloud Devices", to: "/devices" }]}
        userEmail={user?.email}
        picture={user?.picture}
      />

      <div className="w-full h-full">
        <div className="mt-4">
          <div className="w-full h-full px-4 mx-auto space-y-6 sm:max-w-6xl sm:px-8 md:max-w-7xl md:px-12 lg:max-w-8xl">
            <div className="space-y-4">
              <LinkButton
                size="SM"
                theme="blank"
                LeadingIcon={ChevronLeftIcon}
                text="Back to Devices"
                to="/devices"
              />
              <Card className="max-w-3xl p-6">
                <div className="max-w-xl space-y-4">
                  <CardHeader
                    headline={`Deregister ${device.name || device.id} from your cloud account`}
                    description={
                      <>
                        This will remove the device from your cloud account and revoke
                        remote access to it.
                        <br />
                        Please note that local access will still be possible
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
                          text="Cancel"
                          textAlign="center"
                        />
                        <Button
                          size="MD"
                          theme="danger"
                          type="submit"
                          text="Deregister from Cloud"
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
