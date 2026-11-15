"""Offline contract checks for the opt-in Windows Packer QA template."""

from pathlib import Path
import unittest
import xml.etree.ElementTree as ET


ROOT = Path(__file__).resolve().parents[1]
ANSWER = ROOT / "powershell/tests/packer/Autounattend.xml.pkrtpl"


class PackerQATemplateTests(unittest.TestCase):
    def test_answer_file_renders_with_synthetic_inputs(self):
        password = "SyntheticQApassword123456"
        client_address = "10.255.255.1"
        rendered = ANSWER.read_text(encoding="utf-8")
        rendered = rendered.replace("${password}", password)
        rendered = rendered.replace("${client_address}", client_address)

        self.assertNotIn("${", rendered)
        root = ET.fromstring(rendered)
        namespace = {"u": "urn:schemas-microsoft-com:unattend"}
        values = [node.text for node in root.findall(".//u:Password/u:Value", namespace)]
        self.assertEqual(values, [password, password])
        command = root.find(".//u:FirstLogonCommands/u:SynchronousCommand/u:CommandLine", namespace)
        self.assertIsNotNone(command)
        self.assertIn(f"-ClientAddress '{client_address}'", command.text)


if __name__ == "__main__":
    unittest.main()
