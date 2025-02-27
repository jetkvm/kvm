import { redirect } from "react-router-dom";

export function loader() {
  return redirect("/settings/general");
}
