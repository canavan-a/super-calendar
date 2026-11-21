// Which build this is (android/app/build.gradle productFlavors), as in
// super-atlas. Only one for now:
//  sideload  distributed outside Google Play (release/release.sh): the
//            in-app updater.
import { NativeModules } from 'react-native';

export type Distribution = 'sideload';

export const distribution: Distribution = 'sideload';

export const isSideload = distribution === 'sideload';

// The server this build talks to, baked in by Gradle's -PapiUrl (through
// DistributionModule). The fallback reaches a dev server on this computer
// from the Android emulator.
export const apiUrl: string =
  (NativeModules.Distribution?.apiUrl as string | undefined) || 'http://10.0.2.2:8090';
