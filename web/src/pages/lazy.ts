import { lazy } from "react";

// Pages other than the warranty list are downloaded when first visited.
export const Login = lazy(() => import("./Login").then((m) => ({ default: m.Login })));
export const Settings = lazy(() => import("./Settings").then((m) => ({ default: m.Settings })));
export const WarrantyDetail = lazy(() => import("./WarrantyDetail").then((m) => ({ default: m.WarrantyDetail })));
