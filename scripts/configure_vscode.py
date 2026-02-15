#!/usr/bin/env python3
import json
import os

DEFAULT_C_INTELLISENSE_SETTINGS = {
    "configurations": [
        {
            "name": "Linux",
            "includePath": [
                "${workspaceFolder}/**"
            ],
            "defines": [],
            # "compilerPath": "/opt/jetkvm-native-buildkit/bin/arm-rockchip830-linux-uclibcgnueabihf-gcc",
            "cStandard": "c17",
            "cppStandard": "gnu++17",
            "intelliSenseMode": "linux-gcc-arm",
            "configurationProvider": "ms-vscode.cmake-tools"
        }
    ],
    "version": 4
}

def configure_c_intellisense():
    settings_path = os.path.join('.vscode', 'c_cpp_properties.json')
    settings = DEFAULT_C_INTELLISENSE_SETTINGS.copy()
    
    # open existing settings if they exist
    if os.path.exists(settings_path):
        with open(settings_path, 'r') as f:
            settings = json.load(f)
    
    # update compiler path
    settings['configurations'][0]['compilerPath'] = "/opt/jetkvm-native-buildkit/bin/arm-rockchip830-linux-uclibcgnueabihf-gcc"
    settings['configurations'][0]['configurationProvider'] = "ms-vscode.cmake-tools"

    with open(settings_path, 'w') as f:
        json.dump(settings, f, indent=4)

    print("C/C++ IntelliSense configuration updated.")


if __name__ == "__main__":
    configure_c_intellisense()