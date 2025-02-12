import { Fragment, useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowLeftEndOnRectangleIcon, ChevronDownIcon } from "@heroicons/react/16/solid";
import { Menu, MenuButton, Transition } from "@headlessui/react";
import Container from "@/components/Container";
import Card from "@/components/Card";
import { LuMonitorSmartphone } from "react-icons/lu";
import { cx } from "@/cva.config";
import { useHidStore, useRTCStore, useUserStore } from "@/hooks/stores";
import LogoBlueIcon from "@/assets/logo-blue.svg";
import LogoWhiteIcon from "@/assets/logo-white.svg";
import USBStateStatus from "@components/USBStateStatus";
import PeerConnectionStatusCard from "@components/PeerConnectionStatusCard";
import api from "../api";
import { isOnDevice } from "../main";
import { Button, LinkButton } from "./Button";
import { CLOUD_API, SIGNAL_API } from "@/ui.config";

interface NavbarProps {
  isLoggedIn: boolean;
  primaryLinks?: { title: string; to: string }[];
  userEmail?: string;
  showConnectionStatus?: boolean;
  picture?: string;
  kvmName?: string;
}

export default function DashboardNavbar({
  primaryLinks = [],
  isLoggedIn,
  showConnectionStatus,
  userEmail,
  picture,
  kvmName,
}: NavbarProps) {
  const peerConnectionState = useRTCStore(state => state.peerConnection?.connectionState);
  const setUser = useUserStore(state => state.setUser);
  const navigate = useNavigate();
  const onLogout = useCallback(async () => {
    const logoutUrl = isOnDevice
      ? `${SIGNAL_API}/auth/logout`
      : `${CLOUD_API}/logout`;
    const res = await api.POST(logoutUrl);
    if (!res.ok) return;

    setUser(null);
    // The root route will redirect to appropiate login page, be it the local one or the cloud one
    navigate("/");
  }, [navigate, setUser]);

  const usbState = useHidStore(state => state.usbState);

  return (
    <div className="w-full bg-white border-b select-none border-b-slate-800/20 dark:border-b-slate-300/20 dark:bg-slate-900">
      <Container>
        <div className="flex items-center justify-between h-14">
          <div className="flex items-center shrink-0 gap-x-8">
            <div className="inline-block shrink-0">
              <img src={LogoBlueIcon} alt="" className="h-[24px] dark:hidden" />
              <img src={LogoWhiteIcon} alt="" className="hidden h-[24px] dark:block" />
            </div>

            <div className="flex gap-x-2">
              {primaryLinks.map(({ title, to }, i) => {
                return (
                  <LinkButton
                    key={i + title}
                    theme="blank"
                    size="SM"
                    text={title}
                    to={to}
                    LeadingIcon={LuMonitorSmartphone}
                  />
                );
              })}
            </div>
          </div>
          <div className="flex items-center justify-end w-full gap-x-2">
            <div className="flex items-center space-x-4 shrink-0">
              {showConnectionStatus && (
                <div className="items-center hidden gap-x-2 md:flex">
                  <div className="w-[159px]">
                    <PeerConnectionStatusCard
                      state={peerConnectionState}
                      title={kvmName}
                    />
                  </div>
                  <div className="hidden w-[159px] md:block">
                    <USBStateStatus
                      state={usbState}
                      peerConnectionState={peerConnectionState}
                    />
                  </div>
                </div>
              )}
              {isLoggedIn ? (
                <>
                  <hr className="h-[20px] w-[1px] border-none bg-slate-800/20 dark:bg-slate-300/20" />
                  <Menu as="div" className="relative inline-block text-left">
                    <div>
                      <MenuButton as={Fragment}>
                        <Button
                          theme="blank"
                          size="SM"
                          text={
                            <>
                              {picture ? <></> : userEmail}
                              <ChevronDownIcon className="w-4 h-4 shrink-0 text-slate-900 dark:text-white" />
                            </>
                          }
                          LeadingIcon={({ className }) => (
                            picture && (
                              <img
                                src={picture}
                                alt="Avatar"
                                className={cx(
                                className,
                                "h-8 w-8 rounded-full border-2 border-transparent transition-colors group-hover:border-blue-700",
                              )}
                              />
                            )
                          )}
                        />
                      </MenuButton>
                    </div>
                    <Transition
                      as={Fragment}
                      enter="transition ease-in-out duration-75"
                      enterFrom="transform opacity-0"
                      enterTo="transform opacity-100"
                      leave="transition ease-in-out duration-75"
                      leaveFrom="transform opacity-75"
                      leaveTo="transform opacity-0"
                    >
                      <Menu.Items className="absolute right-0 z-50 w-56 mt-2 origin-top-right focus:outline-none">
                        <Card className="overflow-hidden">
                          <div className="p-1 space-y-1 dark:text-white">
                            {userEmail && (
                              <div className="border-b border-b-slate-800/20 dark:border-slate-300/20">
                                <Menu.Item>
                                  <div className="p-2">
                                    <div className="text-xs font-display">
                                      Logged in as
                                    </div>
                                    <div className="w-[200px] truncate font-display text-sm font-semibold">
                                      {userEmail}
                                    </div>
                                  </div>
                                </Menu.Item>
                              </div>
                            )}
                            <div>
                              <Menu.Item>
                                <div onClick={onLogout}>
                                  <button className="block w-full">
                                    <div className="flex items-center gap-x-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:bg-slate-600 dark:hover:bg-slate-700">
                                      <ArrowLeftEndOnRectangleIcon className="w-4 h-4" />
                                      <div className="font-display">Log out</div>
                                    </div>
                                  </button>
                                </div>
                              </Menu.Item>
                            </div>
                          </div>
                        </Card>
                      </Menu.Items>
                    </Transition>
                  </Menu>
                </>
              ) : null}
            </div>
          </div>
        </div>
      </Container>
    </div>
  );
}
