import { LinkButton } from "@/components/Button";
import SimpleNavbar from "@/components/SimpleNavbar";
import Container from "@/components/Container";
import GridBackground from "@components/GridBackground";
import { m } from "@localizations/messages.js";

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
                <h1 className="text-4xl font-semibold text-black dark:text-white">{m.already_adopted_title()}</h1>
                <p className="text-lg text-slate-600 dark:text-slate-400">
                  {m.already_adopted_other_user()}
                </p>
                <p className="mt-4 text-lg text-slate-600 dark:text-slate-400">
                  {m.already_adopted_new_owner()}
                </p>
              </div>

              <div className="text-center">
                <LinkButton
                  to="/devices"
                  size="LG"
                  theme="primary"
                  text={m.already_adopted_return_to_dashboard()}
                />
              </div>
            </div>
          </div>
        </Container>
      </div>
    </>
  );
}
