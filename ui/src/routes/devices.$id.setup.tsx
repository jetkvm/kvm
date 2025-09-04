import { Form, redirect, useActionData, useParams, useSearchParams } from "react-router";
import type { ActionFunction, ActionFunctionArgs, LoaderFunction, LoaderFunctionArgs } from "react-router";

import SimpleNavbar from "@components/SimpleNavbar";
import GridBackground from "@components/GridBackground";
import Container from "@components/Container";
import StepCounter from "@components/StepCounter";
import Fieldset from "@components/Fieldset";
import { InputFieldWithLabel } from "@components/InputField";
import { Button } from "@components/Button";
import { checkAuth } from "@/main";
import { CLOUD_API } from "@/ui.config";

import api from "../api";

const loader: LoaderFunction = async ({ params }: LoaderFunctionArgs) => {
  await checkAuth();
  const res = await fetch(`${CLOUD_API}/devices/${params.id}`, {
    method: "GET",
    mode: "cors",
    credentials: "include",
  });

  if (res.ok) {
    return res.json();
  } else {
    return redirect("/devices");
  }
};

const action: ActionFunction = async ({ request }: ActionFunctionArgs) => {
  // Handle form submission
  const { name, id, returnTo } = Object.fromEntries(await request.formData());
  const res = await api.PUT(`${CLOUD_API}/devices/${id}`, { name });

  if (res.ok) {
    return redirect(returnTo?.toString() ?? `/devices/${id}`);
  } else {
    return { error: "There was an error registering your device" };
  }
};

export default function SetupRoute() {
  const action = useActionData() as { error?: string };
  const { id } = useParams() as { id: string };
  const [sp] = useSearchParams();
  const returnTo = sp.get("returnTo");

  return (
    <>
      <GridBackground />
      <div className="grid min-h-screen grid-rows-(--grid-layout)">
        <SimpleNavbar />
        <Container>
          <div className="flex items-center justify-center w-full h-full isolate">
            <div className="max-w-2xl -mt-32 space-y-8">
              <div className="text-center">
                <StepCounter currStepIdx={1} nSteps={2} />
              </div>

              <div className="space-y-2 text-center">
                <h1 className="text-4xl font-semibold text-black dark:text-white">Let&apos;s name your device</h1>
                <p className="text-slate-600 dark:text-slate-400">
                  Name your device so you can easily identify it later. You can change
                  this name at any time.
                </p>
              </div>

              <Fieldset className="space-y-12">
                <Form method="POST" className="max-w-sm mx-auto space-y-4">
                  <InputFieldWithLabel
                    label="Device Name"
                    type="text"
                    name="name"
                    placeholder="Plex Media Server"
                    autoFocus
                    data-1p-ignore
                    autoComplete="organization"
                    error={action?.error?.toString()}
                  />

                  <input type="hidden" name="id" value={id} />
                  {returnTo && <input type="hidden" name="redirect" value={returnTo} />}
                  <Button
                    size="LG"
                    theme="primary"
                    fullWidth
                    type="submit"
                    text="Finish Setup"
                    textAlign="center"
                  />
                </Form>
              </Fieldset>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}

SetupRoute.loader = loader;
SetupRoute.action = action;
