import Container from "@/components/Container";
import { Link } from "react-router-dom";
import React from "react";
import LogoBlueIcon from "@/assets/logo-blue.png";
import LogoWhiteIcon from "@/assets/logo-white.svg";

type Props = { logoHref?: string; actionElement?: React.ReactNode };

export default function SimpleNavbar({ logoHref, actionElement }: Props) {
  return (
    <div>
      <Container>
        <div className="pb-4 my-4 border-b border-b-800/20 isolate dark:border-b-slate-300/20">
          <div className="flex items-center justify-between">
            <Link to={logoHref ?? "/"} className="hidden h-[26px] dark:inline-block">
              <img src={LogoWhiteIcon} alt="" className="h-[26px] dark:block hidden" />
              <img src={LogoBlueIcon} alt="" className="h-[26px] dark:hidden" />
            </Link>
            <div>{actionElement}</div>
          </div>
        </div>
      </Container>
    </div>
  );
}
