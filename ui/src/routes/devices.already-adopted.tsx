import { LinkButton } from "@/components/Button";
import SimpleNavbar from "@/components/SimpleNavbar";
import Container from "@/components/Container";
import GridBackground from "@components/GridBackground";

export default function DevicesAlreadyAdopted() {
  return (
    <>
      <GridBackground />

      <div className="grid min-h-screen grid-rows-(--grid-layout)">
        <SimpleNavbar />
        <Container>
          <div className="flex items-center justify-center w-full h-full isolate">
            <div className="max-w-2xl -mt-16 space-y-8">
              <div className="space-y-4 text-center">
                <h1 className="text-4xl font-semibold text-black dark:text-white">Device Already Registered</h1>
                <p className="text-lg text-slate-600 dark:text-slate-400">
                  This device is currently registered to another user in our cloud
                  dashboard.
                </p>
                <p className="mt-4 text-lg text-slate-600 dark:text-slate-400">
                  If you&apos;re the new owner, please ask the previous owner to de-register
                  the device from their account in the cloud dashboard. If you believe
                  this is an error, contact our support team for assistance.
                </p>
              </div>

              <div className="text-center">
                <LinkButton
                  to="/devices"
                  size="LG"
                  theme="primary"
                  text="Return to Dashboard"
                />
              </div>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}
